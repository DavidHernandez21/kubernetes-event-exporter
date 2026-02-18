package sinks

import (
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func TestLayoutConvert(t *testing.T) {
	ev := &kube.EnhancedEvent{}
	ev.Namespace = "default"
	ev.Type = "Warning"
	ev.InvolvedObject.Kind = "Pod"
	ev.InvolvedObject.Name = "nginx-server-123abc-456def"
	ev.Message = "Successfully pulled image \"nginx:latest\""
	ev.FirstTimestamp = v1.Time{Time: time.Now()}

	// Because Go, when parsing yaml, its []interface, not []string
	var tagz any
	tagz = make([]any, 2)
	tagz.([]any)[0] = "sre"
	tagz.([]any)[1] = "ops"

	layout := map[string]any{
		"details": map[any]any{
			"message":   "{{ .Message }}",
			"kind":      "{{ .InvolvedObject.Kind }}",
			"name":      "{{ .InvolvedObject.Name }}",
			"namespace": "{{ .Namespace }}",
			"type":      "{{ .Type }}",
			"tags":      tagz,
		},
		"eventType": "kube-event",
		"region":    "us-west-2",
		"createdAt": "{{ .GetTimestampMs }}", // TODO: Test Int casts
	}

	res, err := convertLayoutTemplate(layout, ev)
	require.NoError(t, err)
	require.Equal(t, res["eventType"], "kube-event")

	val, ok := res["details"].(map[string]any)

	require.True(t, ok, "cannot cast to event")

	val2, ok2 := val["message"].(string)
	require.True(t, ok2, "cannot cast message to string")

	require.Equal(t, val2, ev.Message)
}
