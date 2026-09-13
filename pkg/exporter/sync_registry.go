package exporter

import (
	"context"
	"fmt"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/metrics"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/sinks"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SyncRegistry is for development purposes and performs poorly and blocks when an event is received so it is
// not suited for high volume & production workloads
type SyncRegistry struct {
	reg          map[string]sinks.Sink
	MetricsStore *metrics.Store
}

func (s *SyncRegistry) SendEvent(ctx context.Context, name string, event *kube.EnhancedEvent) {
	ctx, span := sinkTracer.Start(ctx, "kubernetes.event.sink", trace.WithSpanKind(trace.SpanKindProducer))
	span.SetAttributes(attribute.String("k8s.event.receiver", name))
	err := s.reg[name].Send(ctx, event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "sink delivery failed")
		span.AddEvent("sink.delivery.error", trace.WithAttributes(attribute.String("error.type", fmt.Sprintf("%T", err))))
		if s.MetricsStore != nil {
			metrics.IncWithTrace(ctx, s.MetricsStore.SendErrors)
		}
		log.Debug().Err(err).Str("sink", name).Str("event", string(event.UID)).Msg("Cannot send event")
	}
	span.End()
}

func (s *SyncRegistry) Register(name string, sink sinks.Sink) {
	if s.reg == nil {
		s.reg = make(map[string]sinks.Sink)
	}

	s.reg[name] = sink
}

func (s *SyncRegistry) Close() {
	for name, sink := range s.reg {
		log.Info().Str("sink", name).Msg("Closing sink")
		sink.Close()
	}
}
