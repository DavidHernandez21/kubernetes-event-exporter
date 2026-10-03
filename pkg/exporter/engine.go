package exporter

import (
	"context"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/DavidHernandez21/kubernetes-event-exporter/pkg/exporter")

const maxTraceMessageRunes = 1024

const emitObjectMetadataEnv = "OTEL_K8S_OBJECT_METADATA"

// Engine is responsible for initializing the receivers from sinks
type Engine struct {
	Registry           ReceiverRegistry
	Route              Route
	emitObjectMetadata bool
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
		Route:              config.Route,
		Registry:           registry,
		emitObjectMetadata: config.EmitObjectMetadata,
	}
}

// OnEvent does not care whether event is add or update. Prior filtering should be done in the controller/watcher
func (e *Engine) OnEvent(event *kube.EnhancedEvent) {
	ctx, span := tracer.Start(context.Background(), "kubernetes.event.process", trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()
	span.SetAttributes(eventAttributes(event, e.emitObjectMetadata)...)

	routeCtx, routeSpan := tracer.Start(ctx, "kubernetes.event.route", trace.WithSpanKind(trace.SpanKindInternal))
	defer routeSpan.End()

	e.Route.ProcessEvent(routeCtx, event, e.Registry)
}

func eventAttributes(event *kube.EnhancedEvent, emitObjectMetadata bool) []attribute.KeyValue {
	message, messageLength, messageTruncated := truncateTraceMessage(event.Message)
	// Reserve space for the 23 fixed attributes and up to 3 optional timestamps.
	attributeCapacity := 23 + 3
	if emitObjectMetadata {
		attributeCapacity += len(event.InvolvedObject.Labels) +
			len(event.InvolvedObject.Annotations) + 1
	}
	attributes := make([]attribute.KeyValue, 0, attributeCapacity)
	attributes = append(attributes,
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
	)
	if emitObjectMetadata {
		attributes = appendObjectMetadataAttributes(attributes, event)
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

func appendObjectMetadataAttributes(attributes []attribute.KeyValue, event *kube.EnhancedEvent) []attribute.KeyValue {
	attributes = append(attributes, stringMapAttributes("k8s.object.label.", event.InvolvedObject.Labels)...)
	attributes = append(attributes, stringMapAttributes("k8s.object.annotation.", event.InvolvedObject.Annotations)...)

	ownerReferences := make([]attribute.Value, 0, len(event.InvolvedObject.OwnerReferences))
	for _, owner := range event.InvolvedObject.OwnerReferences {
		ownerAttributes := []attribute.KeyValue{
			attribute.String("api_version", owner.APIVersion),
			attribute.String("kind", owner.Kind),
			attribute.String("name", owner.Name),
			attribute.String("uid", string(owner.UID)),
		}
		if owner.Controller != nil {
			ownerAttributes = append(ownerAttributes, attribute.Bool("controller", *owner.Controller))
		}
		if owner.BlockOwnerDeletion != nil {
			ownerAttributes = append(ownerAttributes, attribute.Bool("block_owner_deletion", *owner.BlockOwnerDeletion))
		}
		ownerReferences = append(ownerReferences, attribute.Map("owner", ownerAttributes...).Value)
	}
	attributes = append(attributes, attribute.Slice("k8s.object.owner_references", ownerReferences...))

	return attributes
}

func stringMapAttributes(prefix string, values map[string]string) []attribute.KeyValue {
	entries := make([]attribute.KeyValue, 0, len(values))
	for entryKey, value := range values {
		entries = append(entries, attribute.String(prefix+entryKey, value))
	}
	return entries
}

func truncateTraceMessage(message string) (truncated string, length int, wasTruncated bool) {
	length = utf8.RuneCountInString(message)
	if length <= maxTraceMessageRunes {
		return message, length, false
	}

	truncatedRunes := []rune(message)
	return string(truncatedRunes[:maxTraceMessageRunes]), length, true
}

// Stop stops all registered sinks
func (e *Engine) Stop() {
	log.Info().Msg("Closing sinks")
	e.Registry.Close()
	log.Info().Msg("All sinks closed")
}
