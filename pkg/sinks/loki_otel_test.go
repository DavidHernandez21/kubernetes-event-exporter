package sinks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var lokiTestSpanExporter *tracetest.InMemoryExporter

func TestMain(m *testing.M) {
	lokiTestSpanExporter = tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(lokiTestSpanExporter))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	code := m.Run()
	_ = provider.Shutdown(context.Background())
	os.Exit(code)
}

func TestLokiSendEmitsChildHTTPSpan(t *testing.T) {
	lokiTestSpanExporter.Reset()
	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("Traceparent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink, err := NewLoki(&LokiConfig{URL: server.URL})
	require.NoError(t, err)

	parentContext, parentSpan := otel.Tracer("test").Start(context.Background(), "kubernetes.event.sink")
	err = sink.Send(parentContext, &kube.EnhancedEvent{Message: "hello"})
	parentSpan.End()
	require.NoError(t, err)

	spans := lokiTestSpanExporter.GetSpans()
	require.Len(t, spans, 2)
	var parent, httpSpan tracetest.SpanStub
	for _, candidate := range spans {
		if strings.HasPrefix(candidate.Name, "HTTP ") {
			httpSpan = candidate
		} else {
			parent = candidate
		}
	}

	require.Equal(t, parent.SpanContext.TraceID(), httpSpan.SpanContext.TraceID())
	require.Equal(t, parent.SpanContext.SpanID(), httpSpan.Parent.SpanID())
	require.NotEmpty(t, traceparent)
	require.True(t, strings.HasPrefix(traceparent, "00-"))
	assertLokiSpanAttribute(t, httpSpan, "http.request.method", "POST")
	assertLokiSpanAttribute(t, httpSpan, "server.address", "127.0.0.1")
}

func assertLokiSpanAttribute(t *testing.T, span tracetest.SpanStub, key, want string) {
	t.Helper()
	for _, attribute := range span.Attributes {
		if string(attribute.Key) == key {
			require.Equal(t, want, attribute.Value.AsString())
			return
		}
	}
	t.Fatalf("attribute %q not found", key)
}
