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

// ChannelBasedReceiverRegistry creates two channels for each receiver. One is for receiving events and other one is
// for breaking out of the infinite loop. Each message is passed to receivers
// This might not be the best way to implement such feature. A ring buffer can be better
// and we might need a mechanism to drop the vents
// On closing, the registry sends a signal on all exit channels, and then waits for all to complete.
type ChannelBasedReceiverRegistry struct {
	ch           map[string]chan Delivery
	exitCh       map[string]chan struct{}
	wg           *sync.WaitGroup
	MetricsStore *metrics.Store
}

func (r *ChannelBasedReceiverRegistry) SendEvent(ctx context.Context, name string, event *kube.EnhancedEvent) {
	ch := r.ch[name]
	if ch == nil {
		log.Error().Str("name", name).Msg("There is no channel")
	}

	go func() {
		ch <- Delivery{Ctx: ctx, Event: *event}
	}()
}

func (r *ChannelBasedReceiverRegistry) Register(name string, receiver sinks.Sink) {
	if r.ch == nil {
		r.ch = make(map[string]chan Delivery)
		r.exitCh = make(map[string]chan struct{})
	}

	ch := make(chan Delivery)
	exitCh := make(chan struct{})

	r.ch[name] = ch
	r.exitCh[name] = exitCh

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
	for _, ec := range r.exitCh {
		ec <- struct{}{}
	}
	r.wg.Wait()
}
