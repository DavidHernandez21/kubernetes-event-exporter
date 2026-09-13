package exporter

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
)

var testSpanExporter *tracetest.InMemoryExporter

func TestMain(m *testing.M) {
	testSpanExporter = tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(testSpanExporter))
	otel.SetTracerProvider(provider)

	code := m.Run()
	_ = provider.Shutdown(context.Background())
	os.Exit(code)
}

func TestOnEventEmitsEventSpan(t *testing.T) {
	testSpanExporter.Reset()

	engine := &Engine{Route: Route{}, Registry: &testReceiverRegistry{}}
	event := &kube.EnhancedEvent{
		Message:   "pod failed",
		Reason:    "Failed",
		Type:      "Warning",
		Namespace: "default",
		InvolvedObject: kube.EnhancedObjectReference{
			ObjectReference: corev1.ObjectReference{Kind: "Pod", Name: "worker", Namespace: "default"},
		},
	}

	engine.OnEvent(event)

	spans := testSpanExporter.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	require.Equal(t, "kubernetes.event.process", span.Name)
	require.Equal(t, trace.SpanKindConsumer, span.SpanKind)
	assertAttribute(t, span.Attributes, "k8s.event.message", "pod failed")
	assertAttribute(t, span.Attributes, "k8s.event.reason", "Failed")
	assertAttribute(t, span.Attributes, "k8s.object.kind", "Pod")
	assertAttribute(t, span.Attributes, "k8s.object.name", "worker")
}

type failingSink struct{}

func (failingSink) Send(context.Context, *kube.EnhancedEvent) error {
	return errors.New("sink unavailable")
}

func (failingSink) Close() {}

func TestSyncRegistryEmitsChildSinkErrorSpan(t *testing.T) {
	testSpanExporter.Reset()
	registry := &SyncRegistry{}
	registry.Register("loki", failingSink{})

	parentContext, parentSpan := tracer.Start(context.Background(), "test.event")
	registry.SendEvent(parentContext, "loki", &kube.EnhancedEvent{})
	parentSpan.End()

	spans := testSpanExporter.GetSpans()
	require.Len(t, spans, 2)
	var parent, sink tracetest.SpanStub
	for _, candidate := range spans {
		if candidate.Name == "kubernetes.event.sink" {
			sink = candidate
		} else {
			parent = candidate
		}
	}
	require.Equal(t, "kubernetes.event.sink", sink.Name)
	require.Equal(t, parent.SpanContext.TraceID(), sink.SpanContext.TraceID())
	require.Equal(t, parent.SpanContext.SpanID(), sink.Parent.SpanID())
	require.Equal(t, codes.Error, sink.Status.Code)
	require.Len(t, sink.Events, 2)
	require.Equal(t, "exception", sink.Events[0].Name)
	require.Equal(t, "sink.delivery.error", sink.Events[1].Name)
}

func assertAttribute(t *testing.T, attributes []attribute.KeyValue, key string, want string) {
	t.Helper()
	for _, item := range attributes {
		if string(item.Key) == key {
			require.Equal(t, want, item.Value.AsString())
			return
		}
	}
	t.Fatalf("attribute %q not found", key)
}
