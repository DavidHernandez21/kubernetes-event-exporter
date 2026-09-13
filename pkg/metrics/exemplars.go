package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// IncWithTrace records a counter increment and attaches the active span as a
// Prometheus exemplar when the context contains a valid trace.
func IncWithTrace(ctx context.Context, counter prometheus.Counter) {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() || !spanContext.IsSampled() {
		counter.Inc()
		return
	}

	if exemplarCounter, ok := counter.(prometheus.ExemplarAdder); ok {
		exemplarCounter.AddWithExemplar(1, prometheus.Labels{
			"trace_id": spanContext.TraceID().String(),
			"span_id":  spanContext.SpanID().String(),
		})
		return
	}

	counter.Inc()
}
