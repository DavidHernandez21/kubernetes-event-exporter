package sinks

import (
	"context"
	"errors"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type eventbridgeClientMock struct {
	putEvents func(ctx context.Context, input *eventbridge.PutEventsInput) (*eventbridge.PutEventsOutput, error)
}

func (m *eventbridgeClientMock) PutEvents(ctx context.Context, input *eventbridge.PutEventsInput, _ ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error) {
	if m.putEvents == nil {
		return nil, errors.New("put events not implemented")
	}

	return m.putEvents(ctx, input)
}

func TestEventBridgeSinkSendPublishesEvent(t *testing.T) {
	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	cfg := &EventBridgeConfig{
		DetailType:   "deployment",
		Source:       "cd",
		EventBusName: "default",
		Region:       "us-east-1",
	}
	client := &eventbridgeClientMock{
		putEvents: func(_ context.Context, input *eventbridge.PutEventsInput) (*eventbridge.PutEventsOutput, error) {
			require.Len(t, input.Entries, 1)
			entry := input.Entries[0]
			require.Equal(t, cfg.DetailType, aws.ToString(entry.DetailType))
			require.Equal(t, cfg.Source, aws.ToString(entry.Source))
			require.Equal(t, cfg.EventBusName, aws.ToString(entry.EventBusName))
			require.Equal(t, string(ev.ToJSON()), aws.ToString(entry.Detail))
			return &eventbridge.PutEventsOutput{
				Entries: []eventbridgetypes.PutEventsResultEntry{{}},
			}, nil
		},
	}

	sink, err := newEventBridgeSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), ev))
}

func TestEventBridgeSinkSendTemplateError(t *testing.T) {
	cfg := &EventBridgeConfig{
		DetailType:   "deployment",
		Source:       "cd",
		EventBusName: "default",
		Region:       "us-east-1",
		Details: map[string]any{
			"bad": "{{ .Message",
		},
	}

	putCalled := false
	client := &eventbridgeClientMock{
		putEvents: func(_ context.Context, _ *eventbridge.PutEventsInput) (*eventbridge.PutEventsOutput, error) {
			putCalled = true
			return &eventbridge.PutEventsOutput{}, nil
		},
	}

	sink, err := newEventBridgeSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.Error(t, err)
	require.False(t, putCalled)
}

func TestEventBridgeSinkSendPropagatesError(t *testing.T) {
	putErr := errors.New("put events failed")
	cfg := &EventBridgeConfig{
		DetailType:   "deployment",
		Source:       "cd",
		EventBusName: "default",
		Region:       "us-east-1",
	}
	client := &eventbridgeClientMock{
		putEvents: func(_ context.Context, _ *eventbridge.PutEventsInput) (*eventbridge.PutEventsOutput, error) {
			return nil, putErr
		},
	}

	sink, err := newEventBridgeSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.ErrorIs(t, err, putErr)
}
