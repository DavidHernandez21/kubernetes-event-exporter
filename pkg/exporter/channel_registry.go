package exporter

import (
	"context"
	"fmt"
	"sync"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/metrics"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/sinks"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var sinkTracer = otel.Tracer("github.com/DavidHernandez21/kubernetes-event-exporter/pkg/exporter")

// ChannelBasedReceiverRegistry creates a delivery and exit channel for each receiver.
// Bounded queues are opt-in and drop new events when full. On closing, the registry
// sends a signal on all exit channels, and then waits for all receivers to complete.
type ChannelBasedReceiverRegistry struct {
	ch            map[string]chan Delivery
	boundedQueues map[string]bool
	exitCh        map[string]chan struct{}
	wg            *sync.WaitGroup
	MetricsStore  *metrics.Store
}

func (r *ChannelBasedReceiverRegistry) SendEvent(ctx context.Context, name string, event *kube.EnhancedEvent) {
	ch := r.ch[name]
	if ch == nil {
		log.Error().Str("name", name).Msg("There is no channel")
		return
	}

	if r.boundedQueues[name] {
		r.sendBounded(ctx, name, event, ch)
		return
	}

	dispatchBaseCtx := context.WithoutCancel(ctx)
	go func() {
		dispatchCtx, dispatchSpan := sinkTracer.Start(
			dispatchBaseCtx,
			"kubernetes.event.dispatch",
			trace.WithNewRoot(),
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithLinks(trace.LinkFromContext(ctx)),
		)
		dispatchSpan.SetAttributes(attribute.String("k8s.event.receiver", name))
		defer dispatchSpan.End()

		ch <- Delivery{Ctx: dispatchCtx, Event: *event}
	}()
}

func (r *ChannelBasedReceiverRegistry) sendBounded(ctx context.Context, name string, event *kube.EnhancedEvent, ch chan Delivery) {
	dispatchBaseCtx := context.WithoutCancel(ctx)
	dispatchCtx, dispatchSpan := sinkTracer.Start(
		dispatchBaseCtx,
		"kubernetes.event.dispatch",
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithLinks(trace.LinkFromContext(ctx)),
	)
	defer dispatchSpan.End()

	dispatchSpan.SetAttributes(attribute.String("k8s.event.receiver", name))
	queueAttributes := func(outcome string) []attribute.KeyValue {
		return []attribute.KeyValue{
			attribute.String("k8s.event.receiver", name),
			attribute.Int("k8s.event.queue.capacity", cap(ch)),
			attribute.Int("k8s.event.queue.length", len(ch)),
			attribute.String("k8s.event.queue.outcome", outcome),
		}
	}

	select {
	case ch <- Delivery{Ctx: dispatchCtx, Event: *event}:
		dispatchSpan.AddEvent("receiver.queue.enqueue", trace.WithAttributes(queueAttributes("enqueued")...))
	default:
		attributes := queueAttributes("dropped")
		dispatchSpan.AddEvent("receiver.queue.enqueue", trace.WithAttributes(attributes...))
		dispatchSpan.AddEvent("receiver.queue.full", trace.WithAttributes(attributes...))
		dispatchSpan.SetStatus(codes.Error, "receiver queue full")
		if r.MetricsStore != nil {
			metrics.IncWithTrace(dispatchCtx, r.MetricsStore.ReceiverQueueDrops.WithLabelValues(name))
		}
		log.Warn().Str("sink", name).Str("event", event.Message).Int("queueCapacity", cap(ch)).Msg("Dropping event because receiver queue is full")
	}
}

func (r *ChannelBasedReceiverRegistry) Register(name string, receiver sinks.Sink, options ReceiverOptions) {
	if r.ch == nil {
		r.ch = make(map[string]chan Delivery)
		r.boundedQueues = make(map[string]bool)
		r.exitCh = make(map[string]chan struct{})
	}

	ch := make(chan Delivery)
	if options.EnableBoundedQueue {
		ch = make(chan Delivery, options.QueueCapacity)
	}
	exitCh := make(chan struct{})

	r.ch[name] = ch
	r.boundedQueues[name] = options.EnableBoundedQueue
	r.exitCh[name] = exitCh
	if options.EnableBoundedQueue {
		log.Info().Str("sink", name).Int("queueCapacity", options.QueueCapacity).Msg("Bounded receiver queue enabled")
	}

	if r.wg == nil {
		r.wg = &sync.WaitGroup{}
	}

	r.wg.Go(func() {
	Loop:
		for {
			select {
			case delivery := <-ch:
				log.Debug().Str("sink", name).Str("event", delivery.Event.Message).Msg("sending event to sink")
				ctx, span := sinkTracer.Start(delivery.Ctx, "kubernetes.event.sink", trace.WithSpanKind(trace.SpanKindProducer))
				span.SetAttributes(attribute.String("k8s.event.receiver", name))
				err := receiver.Send(ctx, &delivery.Event)
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, "sink delivery failed")
					span.AddEvent("sink.delivery.error", trace.WithAttributes(attribute.String("error.type", fmt.Sprintf("%T", err))))
					metrics.IncWithTrace(ctx, r.MetricsStore.SendErrors)
					log.Debug().Err(err).Str("sink", name).Str("event", delivery.Event.Message).Msg("Cannot send event")
				}
				span.End()
			case <-exitCh:
				log.Info().Str("sink", name).Msg("Closing the sink")
				break Loop
			}
		}
		receiver.Close()
		log.Info().Str("sink", name).Msg("Closed")
	})
}

// Close signals closing to all sinks and waits for them to complete.
// The wait could block indefinitely depending on the sink implementations.
func (r *ChannelBasedReceiverRegistry) Close() {
	// Send exit command and wait for exit of all sinks
	for name, ec := range r.exitCh {
		if queued := len(r.ch[name]); queued > 0 {
			log.Warn().Str("sink", name).Int("queued", queued).Msg("Closing sink with queued events that may be discarded")
		}
		ec <- struct{}{}
	}
	r.wg.Wait()
}
