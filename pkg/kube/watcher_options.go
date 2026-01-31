package kube

import (
	"fmt"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/metrics"
)

// EventWatcherOption defines a functional option for configuring the EventWatcher
// If validation of options fails, an error is returned
type EventWatcherOption func(*eventWatcherConfig) error

// eventWatcherOptions holds the configuration options for EventWatcher
type eventWatcherOptions struct {
}

// eventWatcherConfig combines required and optional configuration for EventWatcher
type eventWatcherConfig struct {
	eventWatcherOptions
	eventWatcherRequired
}

// EventWatcherRequired holds the required configuration options for EventWatcher
type eventWatcherRequired struct {
	metricsStore       *metrics.Store
	onEvent            func(*EnhancedEvent)
	namespace          string
	maxEventAgeSeconds int64
	cacheSize          int
	mappingCacheSize   int
	cacheTTL           time.Duration
	omitLookup         bool
}

// WithMetricsStore sets the MetricsStore for the EventWatcher
func WithMetricsStore(store *metrics.Store) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		if store == nil {
			return fmt.Errorf("WithMetricsStore: store cannot be nil")
		}
		o.metricsStore = store
		return nil
	}
}

// WithOnEventHandler sets the OnEvent handler for the EventWatcher
func WithOnEventHandler(handler func(*EnhancedEvent)) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		if handler == nil {
			return fmt.Errorf("WithOnEventHandler: handler cannot be nil")
		}
		o.onEvent = handler
		return nil
	}
}

// WithNamespace sets the namespace to watch for events
func WithNamespace(namespace string) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		o.namespace = namespace
		return nil
	}
}

// WithMaxEventAgeSeconds sets the maximum age of events to process
func WithMaxEventAgeSeconds(age int64) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		if age <= 0 {
			return fmt.Errorf("WithMaxEventAgeSeconds: age must be positive")
		}
		o.maxEventAgeSeconds = age
		return nil
	}
}

// WithCacheSize sets the size of the object metadata cache
func WithCacheSize(size int) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		if size <= 0 {
			return fmt.Errorf("WithCacheSize: size must be positive")
		}
		o.cacheSize = size
		return nil
	}
}

// WithMappingCacheSize sets the size of the mapping cache
func WithMappingCacheSize(size int) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		if size <= 0 {
			return fmt.Errorf("WithMappingCacheSize: size must be positive")
		}
		o.mappingCacheSize = size
		return nil
	}
}

// WithCacheTTL sets the TTL for the object metadata cache
func WithCacheTTL(ttl time.Duration) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		if ttl <= 0 {
			return fmt.Errorf("WithCacheTTL: ttl must be positive")
		}
		o.cacheTTL = ttl
		return nil
	}
}

// WithOmitLookup sets whether to omit lookups for object metadata
func WithOmitLookup(omit bool) EventWatcherOption {
	return func(o *eventWatcherConfig) error {
		o.omitLookup = omit
		return nil
	}
}

// NewEventWatcherRequired constructs an EventWatcherRequired instance using the provided options
// It returns an error if any required options are missing or invalid
func NewEventWatcherRequired(opts ...EventWatcherOption) (*eventWatcherRequired, error) {
	var o eventWatcherConfig
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			return nil, err
		}
	}

	return &o.eventWatcherRequired, nil
}
