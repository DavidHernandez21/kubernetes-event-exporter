package sinks

import (
	"context"
	"errors"
	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type kinesisClientMock struct {
	putRecord func(ctx context.Context, input *kinesis.PutRecordInput) (*kinesis.PutRecordOutput, error)
}

func (m *kinesisClientMock) PutRecord(ctx context.Context, input *kinesis.PutRecordInput, _ ...func(*kinesis.Options)) (*kinesis.PutRecordOutput, error) {
	if m.putRecord == nil {
		return nil, errors.New("put record not implemented")
	}

	return m.putRecord(ctx, input)
}

func TestKinesisSinkSendPublishesRecord(t *testing.T) {
	ev := &kube.EnhancedEvent{Event: corev1.Event{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-1")},
		Message:    "hello",
	}}
	cfg := &KinesisConfig{StreamName: "kube-events", Region: "us-east-1"}
	client := &kinesisClientMock{
		putRecord: func(_ context.Context, input *kinesis.PutRecordInput) (*kinesis.PutRecordOutput, error) {
			require.Equal(t, cfg.StreamName, aws.ToString(input.StreamName))
			require.Equal(t, "uid-1", aws.ToString(input.PartitionKey))
			require.Equal(t, string(ev.ToJSON()), string(input.Data))
			return &kinesis.PutRecordOutput{}, nil
		},
	}

	sink, err := newKinesisSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), ev))
}

func TestKinesisSinkSendTemplateError(t *testing.T) {
	cfg := &KinesisConfig{
		StreamName: "kube-events",
		Region:     "us-east-1",
		Layout: map[string]any{
			"bad": "{{ .Message",
		},
	}

	putCalled := false
	client := &kinesisClientMock{
		putRecord: func(_ context.Context, _ *kinesis.PutRecordInput) (*kinesis.PutRecordOutput, error) {
			putCalled = true
			return &kinesis.PutRecordOutput{}, nil
		},
	}

	sink, err := newKinesisSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.Error(t, err)
	require.False(t, putCalled)
}

func TestKinesisSinkSendPropagatesError(t *testing.T) {
	putErr := errors.New("put record failed")
	cfg := &KinesisConfig{StreamName: "kube-events", Region: "us-east-1"}
	client := &kinesisClientMock{
		putRecord: func(_ context.Context, _ *kinesis.PutRecordInput) (*kinesis.PutRecordOutput, error) {
			return nil, putErr
		},
	}

	sink, err := newKinesisSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}})
	require.ErrorIs(t, err, putErr)
}
