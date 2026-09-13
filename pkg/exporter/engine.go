package exporter

import (
	"context"
	"reflect"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/DavidHernandez21/kubernetes-event-exporter/pkg/exporter")

const maxTraceMessageRunes = 1024

// Engine is responsible for initializing the receivers from sinks
type Engine struct {
	Registry ReceiverRegistry
	Route    Route
}

func NewEngine(config *Config, registry ReceiverRegistry) *Engine {
	for i := range config.Receivers {
		v := &config.Receivers[i]
		sink, err := v.GetSink()
		if err != nil {
			log.Fatal().Err(err).Str("name", v.Name).Msg("Cannot initialize sink")
		}

		log.Info().
			Str("name", v.Name).
			Str("type", reflect.TypeOf(sink).String()).
			Msg("Registering sink")

		registry.Register(v.Name, sink)
	}

	return &Engine{
		Route:    config.Route,
		Registry: registry,
	}
}

// OnEvent does not care whether event is add or update. Prior filtering should be done in the controller/watcher
func (e *Engine) OnEvent(event *kube.EnhancedEvent) {
	ctx, span := tracer.Start(context.Background(), "kubernetes.event.process", trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()
	span.SetAttributes(eventAttributes(event)...)

	e.Route.ProcessEvent(ctx, event, e.Registry)
}

func eventAttributes(event *kube.EnhancedEvent) []attribute.KeyValue {
	message, messageLength, messageTruncated := truncateTraceMessage(event.Message)
	attributes := []attribute.KeyValue{
		attribute.String("k8s.event.name", event.Name),
		attribute.String("k8s.event.uid", string(event.UID)),
		attribute.String("k8s.event.namespace.name", event.Namespace),
		attribute.String("k8s.event.type", event.Type),
		attribute.String("k8s.event.reason", event.Reason),
		attribute.String("k8s.event.message", message),
		attribute.Int("k8s.event.message.length", messageLength),
		attribute.Bool("k8s.event.message.truncated", messageTruncated),
		attribute.String("k8s.event.action", event.Action),
		attribute.String("k8s.event.source.component", event.Source.Component),
		attribute.String("k8s.event.source.host", event.Source.Host),
		attribute.String("k8s.event.reporting_controller", event.ReportingController),
		attribute.String("k8s.event.reporting_instance", event.ReportingInstance),
		attribute.Int("k8s.event.count", int(event.Count)),
		attribute.String("k8s.object.kind", event.InvolvedObject.Kind),
		attribute.String("k8s.object.name", event.InvolvedObject.Name),
		attribute.String("k8s.object.namespace.name", event.InvolvedObject.Namespace),
		attribute.String("k8s.object.uid", string(event.InvolvedObject.UID)),
		attribute.String("k8s.object.api_version", event.InvolvedObject.APIVersion),
		attribute.Bool("k8s.object.deleted", event.InvolvedObject.Deleted),
		attribute.Int("k8s.object.owner_reference.count", len(event.InvolvedObject.OwnerReferences)),
		attribute.Int("k8s.object.label.count", len(event.InvolvedObject.Labels)),
		attribute.Int("k8s.object.annotation.count", len(event.InvolvedObject.Annotations)),
	}

	if !event.FirstTimestamp.IsZero() {
		attributes = append(attributes, attribute.String("k8s.event.first_timestamp", event.FirstTimestamp.UTC().Format(time.RFC3339Nano)))
	}
	if !event.LastTimestamp.IsZero() {
		attributes = append(attributes, attribute.String("k8s.event.last_timestamp", event.LastTimestamp.UTC().Format(time.RFC3339Nano)))
	}
	if !event.EventTime.IsZero() {
		attributes = append(attributes, attribute.String("k8s.event.event_time", event.EventTime.UTC().Format(time.RFC3339Nano)))
	}

	return attributes
}

func truncateTraceMessage(message string) (truncated string, length int, wasTruncated bool) {
	runes := []rune(message)
	if len(runes) <= maxTraceMessageRunes {
		return message, len(runes), false
	}

	return string(runes[:maxTraceMessageRunes]), len(runes), true
}

// Stop stops all registered sinks
func (e *Engine) Stop() {
	log.Info().Msg("Closing sinks")
	e.Registry.Close()
	log.Info().Msg("All sinks closed")
}
