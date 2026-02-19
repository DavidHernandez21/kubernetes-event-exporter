package kube

import (
	"fmt"
	"sync"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/metrics"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var startUpTime = time.Now()

type eventHandler func(event *EnhancedEvent)

type eventWatcher struct {
	informer            cache.SharedInformer
	objectMetadataCache objectMetadataProvider
	stopper             chan struct{}
	fn                  eventHandler
	metricsStore        *metrics.Store
	dynamicClient       *dynamic.DynamicClient
	clientset           *kubernetes.Clientset
	wg                  sync.WaitGroup
	maxEventAgeSeconds  time.Duration
	omitLookup          bool
}

func NewEventWatcher(config *rest.Config, required *eventWatcherRequired, opts ...EventWatcherOption) (*eventWatcher, error) {
	var o eventWatcherConfig
	o.eventWatcherRequired = *required

	for _, opt := range opts {
		if err := opt(&o); err != nil {
			return nil, fmt.Errorf("applying option failed: %w", err)
		}
	}

	clientset := kubernetes.NewForConfigOrDie(config)
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithNamespace(o.namespace))
	informer := factory.Core().V1().Events().Informer()

	watcher := &eventWatcher{
		informer:            informer,
		stopper:             make(chan struct{}),
		objectMetadataCache: newObjectMetadataProviderWithTTL(o.cacheSize, o.mappingCacheSize, o.cacheTTL),
		omitLookup:          o.omitLookup,
		fn:                  o.onEvent,
		maxEventAgeSeconds:  time.Second * time.Duration(o.maxEventAgeSeconds),
		metricsStore:        o.metricsStore,
		dynamicClient:       dynamic.NewForConfigOrDie(config),
		clientset:           clientset,
	}

	// Register watcher as ResourceEventHandler to process adds, updates, deletes
	_, err := informer.AddEventHandler(watcher)
	if err != nil {
		return nil, fmt.Errorf("failed to add event handler: %w", err)
	}

	if err := informer.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		watcher.metricsStore.WatchErrors.Inc()
	}); err != nil {
		return nil, fmt.Errorf("failed to set watch error handler: %w", err)
	}

	return watcher, nil
}

//nolint:errcheck
func (e *eventWatcher) OnAdd(obj any, isInInitialList bool) {
	// ignore type assertion failure
	event := obj.(*corev1.Event)
	e.onEvent(event)
}

// OnUpdate is called when an existing Event is modified
//
//nolint:errcheck
func (e *eventWatcher) OnUpdate(oldObj, newObj any) {
	event := newObj.(*corev1.Event)
	e.onEvent(event)
}

// Ignore events older than the maxEventAgeSeconds
func (e *eventWatcher) isEventDiscarded(event *corev1.Event) bool {
	// Use the most recent timestamp: series, then LastTimestamp, then EventTime
	var timestamp time.Time
	if event.Series != nil && !event.Series.LastObservedTime.Time.IsZero() {
		timestamp = event.Series.LastObservedTime.Time
	} else if !event.LastTimestamp.Time.IsZero() {
		timestamp = event.LastTimestamp.Time
	} else {
		timestamp = event.EventTime.Time
	}
	eventAge := time.Since(timestamp)
	if eventAge > e.maxEventAgeSeconds {
		// Log discarded events if they were created after the watcher started
		// (to suppress warnings from initial synchronization)
		if timestamp.After(startUpTime) {
			log.Warn().
				Str("event age", eventAge.String()).
				Str("event namespace", event.Namespace).
				Str("event name", event.Name).
				Msg("Event discarded as being older than maxEventAgeSeconds")
			e.metricsStore.EventsDiscarded.Inc()
		}
		return true
	}
	return false
}

func (e *eventWatcher) onEvent(event *corev1.Event) {
	if e.isEventDiscarded(event) {
		return
	}

	log.Debug().
		Str("msg", event.Message).
		Str("namespace", event.Namespace).
		Str("reason", event.Reason).
		Str("involvedObject", event.InvolvedObject.Name).
		Msg("Received event")

	e.metricsStore.EventsProcessed.Inc()

	ev := &EnhancedEvent{
		Event: *event.DeepCopy(),
	}
	ev.Event.ManagedFields = nil

	if e.omitLookup {
		ev.InvolvedObject.ObjectReference = *event.InvolvedObject.DeepCopy()
	} else {
		om, err := e.objectMetadataCache.getObjectMetadata(&event.InvolvedObject, e.clientset, e.dynamicClient, e.metricsStore)
		if err != nil {
			if errors.IsNotFound(err) {
				ev.InvolvedObject.Deleted = true
				log.Error().Err(err).Msg("Object not found, likely deleted")
			} else {
				log.Error().Err(err).Msg("Failed to get object metadata")
			}
			ev.InvolvedObject.ObjectReference = *event.InvolvedObject.DeepCopy()
		} else {
			ev.InvolvedObject.Labels = om.Labels
			ev.InvolvedObject.Annotations = om.Annotations
			ev.InvolvedObject.OwnerReferences = om.OwnerReferences
			ev.InvolvedObject.ObjectReference = *event.InvolvedObject.DeepCopy()
			ev.InvolvedObject.Deleted = om.Deleted
		}
	}

	e.fn(ev)
}

func (e *eventWatcher) OnDelete(obj any) {
	// Ignore deletes
}

func (e *eventWatcher) Start() {
	e.wg.Go(func() {
		e.informer.Run(e.stopper)
	})
}

func (e *eventWatcher) Stop() {
	close(e.stopper)
	e.wg.Wait()
}

func (e *eventWatcher) setStartUpTime(t time.Time) {
	startUpTime = t
}
