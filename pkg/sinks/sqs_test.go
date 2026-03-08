package sinks

import (
	"context"
	"errors"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type sqsClientMock struct {
	getQueueUrl func(ctx context.Context, input *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error)
	sendMessage func(ctx context.Context, input *sqs.SendMessageInput) (*sqs.SendMessageOutput, error)
}

func (m *sqsClientMock) GetQueueUrl(ctx context.Context, input *sqs.GetQueueUrlInput, _ ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	if m.getQueueUrl == nil {
		return nil, errors.New("get queue url not implemented")
	}
	return m.getQueueUrl(ctx, input)
}

func (m *sqsClientMock) SendMessage(ctx context.Context, input *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	if m.sendMessage == nil {
		return nil, errors.New("send message not implemented")
	}
	return m.sendMessage(ctx, input)
}

func TestNewSQSSinkResolvesQueueURL(t *testing.T) {
	cfg := &SQSConfig{QueueName: "events", Region: "us-east-1"}
	client := &sqsClientMock{
		getQueueUrl: func(_ context.Context, input *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error) {
			return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("http://queue-url")}, nil
		},
	}

	sink, err := newSQSSinkWithClient(context.Background(), cfg, client)
	require.NoError(t, err)
	require.Equal(t, "http://queue-url", sink.(*SQSSink).queueURL)
}

func TestSQSSinkSendPublishesMessage(t *testing.T) {
	cfg := &SQSConfig{QueueName: "events", Region: "us-east-1"}
	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	client := &sqsClientMock{}
	client.getQueueUrl = func(_ context.Context, _ *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error) {
		return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("http://queue-url")}, nil
	}
	client.sendMessage = func(_ context.Context, input *sqs.SendMessageInput) (*sqs.SendMessageOutput, error) {
		require.Equal(t, "http://queue-url", aws.ToString(input.QueueUrl))
		require.Equal(t, string(ev.ToJSON()), aws.ToString(input.MessageBody))
		return &sqs.SendMessageOutput{}, nil
	}

	sink, err := newSQSSinkWithClient(context.Background(), cfg, client)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), ev))
}

func TestSQSSinkSendTemplateError(t *testing.T) {
	cfg := &SQSConfig{
		QueueName: "events",
		Region:    "us-east-1",
		Layout: map[string]any{
			"bad": "{{ .Message",
		},
	}
	client := &sqsClientMock{}
	sendCalled := false
	client.getQueueUrl = func(_ context.Context, _ *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error) {
		return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("http://queue-url")}, nil
	}
	client.sendMessage = func(_ context.Context, _ *sqs.SendMessageInput) (*sqs.SendMessageOutput, error) {
		sendCalled = true
		return &sqs.SendMessageOutput{}, nil
	}

	sink, err := newSQSSinkWithClient(context.Background(), cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.Error(t, err)
	require.False(t, sendCalled)
}

func TestSQSSinkSendPropagatesError(t *testing.T) {
	sendErr := errors.New("send failed")
	cfg := &SQSConfig{QueueName: "events", Region: "us-east-1"}
	client := &sqsClientMock{}
	client.getQueueUrl = func(_ context.Context, _ *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error) {
		return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("http://queue-url")}, nil
	}
	client.sendMessage = func(_ context.Context, _ *sqs.SendMessageInput) (*sqs.SendMessageOutput, error) {
		return nil, sendErr
	}

	sink, err := newSQSSinkWithClient(context.Background(), cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.ErrorIs(t, err, sendErr)
}
