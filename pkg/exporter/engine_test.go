package exporter

import (
	"os"
	"strings"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/sinks"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestTruncateTraceMessage(t *testing.T) {
	message := strings.Repeat("é", maxTraceMessageRunes+1)

	truncated, length, wasTruncated := truncateTraceMessage(message)

	assert.Equal(t, maxTraceMessageRunes, len([]rune(truncated)))
	assert.Equal(t, maxTraceMessageRunes+1, length)
	assert.True(t, wasTruncated)
	assert.True(t, strings.HasPrefix(message, truncated))
}

func TestEventAttributesObjectMetadataDisabled(t *testing.T) {
	unsetEnvironmentVariable(t, emitObjectMetadataEnv)

	event := &kube.EnhancedEvent{
		InvolvedObject: kube.EnhancedObjectReference{
			Labels:      map[string]string{"app": "exporter"},
			Annotations: map[string]string{"team": "platform"},
		},
	}

	attributes := attributesByKey(eventAttributes(event, false))

	assert.NotContains(t, attributes, "k8s.object.label.app")
	assert.NotContains(t, attributes, "k8s.object.annotation.team")
	assert.NotContains(t, attributes, "k8s.object.owner_references")
}

func TestEventAttributesObjectMetadata(t *testing.T) {
	t.Setenv(emitObjectMetadataEnv, "true")
	controller := true
	blockOwnerDeletion := false
	event := &kube.EnhancedEvent{
		InvolvedObject: kube.EnhancedObjectReference{
			Labels:      map[string]string{"app": "exporter"},
			Annotations: map[string]string{"team": "platform"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "apps/v1",
				Kind:               "Deployment",
				Name:               "event-exporter",
				UID:                types.UID("owner-uid"),
				Controller:         &controller,
				BlockOwnerDeletion: &blockOwnerDeletion,
			}},
		},
	}

	attributes := attributesByKey(eventAttributes(event, true))

	assert.Equal(t, "exporter", attributes["k8s.object.label.app"].AsString())
	assert.Equal(t, "platform", attributes["k8s.object.annotation.team"].AsString())

	owners := attributes["k8s.object.owner_references"]
	assert.Equal(t, attribute.SLICE, owners.Type())
	assert.Len(t, owners.AsSlice(), 1)
	owner := owners.AsSlice()[0]
	assert.Equal(t, attribute.MAP, owner.Type())
	ownerAttributes := attributesByKey(owner.AsMap())
	assert.Equal(t, "apps/v1", ownerAttributes["api_version"].AsString())
	assert.Equal(t, "Deployment", ownerAttributes["kind"].AsString())
	assert.Equal(t, "event-exporter", ownerAttributes["name"].AsString())
	assert.Equal(t, "owner-uid", ownerAttributes["uid"].AsString())
	assert.True(t, ownerAttributes["controller"].AsBool())
	assert.False(t, ownerAttributes["block_owner_deletion"].AsBool())
}

func attributesByKey(attributes []attribute.KeyValue) map[string]attribute.Value {
	values := make(map[string]attribute.Value, len(attributes))
	for _, attribute := range attributes {
		values[string(attribute.Key)] = attribute.Value
	}
	return values
}

func unsetEnvironmentVariable(t *testing.T, key string) {
	t.Helper()
	value, exists := os.LookupEnv(key)
	assert.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestEngineNoRoutes(t *testing.T) {
	cfg := &Config{
		Route:     Route{},
		Receivers: nil,
	}

	e := NewEngine(cfg, &SyncRegistry{})
	ev := &kube.EnhancedEvent{}
	e.OnEvent(ev)
}

func TestEngineSimple(t *testing.T) {
	config := &sinks.InMemoryConfig{}
	cfg := &Config{
		Route: Route{
			Match: []Rule{{
				Receiver: "in-mem",
			}},
		},
		Receivers: []sinks.ReceiverConfig{{
			Name:     "in-mem",
			InMemory: config,
		}},
	}

	e := NewEngine(cfg, &SyncRegistry{})
	ev := &kube.EnhancedEvent{}
	e.OnEvent(ev)

	assert.Contains(t, config.Ref.Events, ev)
}

func TestEngineDropSimple(t *testing.T) {
	config := &sinks.InMemoryConfig{}
	cfg := &Config{
		Route: Route{
			Drop: []Rule{{
				// Drops anything
			}},
			Match: []Rule{{
				Receiver: "in-mem",
			}},
		},
		Receivers: []sinks.ReceiverConfig{{
			Name:     "in-mem",
			InMemory: config,
		}},
	}

	e := NewEngine(cfg, &SyncRegistry{})
	ev := &kube.EnhancedEvent{}
	e.OnEvent(ev)

	assert.NotContains(t, config.Ref.Events, ev)
	assert.Empty(t, config.Ref.Events)
}
