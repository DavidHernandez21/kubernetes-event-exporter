package sinks

import (
	"context"
	"errors"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type snsClientMock struct {
	publish func(ctx context.Context, input *sns.PublishInput) (*sns.PublishOutput, error)
}

func (m *snsClientMock) Publish(ctx context.Context, input *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	if m.publish == nil {
		return nil, errors.New("publish not implemented")
	}
	return m.publish(ctx, input)
}

func TestSNSSinkSendPublishesMessage(t *testing.T) {
	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	cfg := &SNSConfig{TopicARN: "arn:aws:sns:us-east-1:000000000000:kube-events", Region: "us-east-1"}
	client := &snsClientMock{
		publish: func(_ context.Context, input *sns.PublishInput) (*sns.PublishOutput, error) {
			require.Equal(t, cfg.TopicARN, aws.ToString(input.TopicArn))
			require.Equal(t, string(ev.ToJSON()), aws.ToString(input.Message))
			return &sns.PublishOutput{}, nil
		},
	}

	sink, err := newSNSSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), ev))
}

func TestSNSSinkSendTemplateError(t *testing.T) {
	cfg := &SNSConfig{
		TopicARN: "arn:aws:sns:us-east-1:000000000000:kube-events",
		Region:   "us-east-1",
		Layout: map[string]any{
			"bad": "{{ .Message",
		},
	}

	publishCalled := false
	client := &snsClientMock{
		publish: func(_ context.Context, _ *sns.PublishInput) (*sns.PublishOutput, error) {
			publishCalled = true
			return &sns.PublishOutput{}, nil
		},
	}

	sink, err := newSNSSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.Error(t, err)
	require.False(t, publishCalled)
}

func TestSNSSinkSendPropagatesError(t *testing.T) {
	publishErr := errors.New("publish failed")
	cfg := &SNSConfig{TopicARN: "arn:aws:sns:us-east-1:000000000000:kube-events", Region: "us-east-1"}
	client := &snsClientMock{
		publish: func(_ context.Context, _ *sns.PublishInput) (*sns.PublishOutput, error) {
			return nil, publishErr
		},
	}

	sink, err := newSNSSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.ErrorIs(t, err, publishErr)
}
