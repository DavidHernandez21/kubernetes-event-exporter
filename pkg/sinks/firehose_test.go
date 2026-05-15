package sinks

import (
	"context"
	"errors"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type firehoseClientMock struct {
	putRecord func(ctx context.Context, input *firehose.PutRecordInput) (*firehose.PutRecordOutput, error)
}

func (m *firehoseClientMock) PutRecord(ctx context.Context, input *firehose.PutRecordInput, _ ...func(*firehose.Options)) (*firehose.PutRecordOutput, error) {
	if m.putRecord == nil {
		return nil, errors.New("put record not implemented")
	}

	return m.putRecord(ctx, input)
}

func TestFirehoseSinkSendPublishesRecord(t *testing.T) {
	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	cfg := &FirehoseConfig{DeliveryStreamName: "kube-events", Region: "us-east-1"}
	client := &firehoseClientMock{
		putRecord: func(_ context.Context, input *firehose.PutRecordInput) (*firehose.PutRecordOutput, error) {
			require.Equal(t, cfg.DeliveryStreamName, aws.ToString(input.DeliveryStreamName))
			require.Equal(t, string(ev.ToJSON()), string(input.Record.Data))
			return &firehose.PutRecordOutput{}, nil
		},
	}

	sink, err := newFirehoseSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), ev))
}

func TestFirehoseSinkSendTemplateError(t *testing.T) {
	cfg := &FirehoseConfig{
		DeliveryStreamName: "kube-events",
		Region:             "us-east-1",
		Layout: map[string]any{
			"bad": "{{ .Message",
		},
	}

	putCalled := false
	client := &firehoseClientMock{
		putRecord: func(_ context.Context, _ *firehose.PutRecordInput) (*firehose.PutRecordOutput, error) {
			putCalled = true
			return &firehose.PutRecordOutput{}, nil
		},
	}

	sink, err := newFirehoseSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.Error(t, err)
	require.False(t, putCalled)
}

func TestFirehoseSinkSendPropagatesError(t *testing.T) {
	putErr := errors.New("put record failed")
	cfg := &FirehoseConfig{DeliveryStreamName: "kube-events", Region: "us-east-1"}
	client := &firehoseClientMock{
		putRecord: func(_ context.Context, _ *firehose.PutRecordInput) (*firehose.PutRecordOutput, error) {
			return nil, putErr
		},
	}

	sink, err := newFirehoseSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.ErrorIs(t, err, putErr)
}
