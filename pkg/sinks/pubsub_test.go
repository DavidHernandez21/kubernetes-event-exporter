package sinks

import (
	"context"
	"errors"
	"testing"

	pubsub "cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type publishResultMock struct {
	id  string
	err error
}

func (r *publishResultMock) Get(_ context.Context) (string, error) {
	return r.id, r.err
}

type publisherMock struct {
	publish func(ctx context.Context, msg *pubsub.Message) publishResultAPI
	stop    func()
}

func (p *publisherMock) Publish(ctx context.Context, msg *pubsub.Message) publishResultAPI {
	if p.publish == nil {
		return &publishResultMock{err: errors.New("publish not implemented")}
	}
	return p.publish(ctx, msg)
}

func (p *publisherMock) Stop() {
	if p.stop != nil {
		p.stop()
	}
}

type pubsubClientMock struct {
	createTopic func(ctx context.Context, topic *pubsubpb.Topic) error
	publisher   func(topicNameOrID string) publisherAPI
	close       func() error
}

func (c *pubsubClientMock) CreateTopic(ctx context.Context, topic *pubsubpb.Topic) error {
	if c.createTopic == nil {
		return nil
	}
	return c.createTopic(ctx, topic)
}

func (c *pubsubClientMock) Publisher(topicNameOrID string) publisherAPI {
	if c.publisher == nil {
		return &publisherMock{}
	}
	return c.publisher(topicNameOrID)
}

func (c *pubsubClientMock) Close() error {
	if c.close == nil {
		return nil
	}
	return c.close()
}

func TestNewPubsubSinkWithClientCreatesTopicWhenEnabled(t *testing.T) {
	cfg := &PubsubConfig{GcloudProjectId: "my-project", Topic: "my-topic", CreateTopic: true}
	createdTopic := ""
	requestedPublisher := ""
	client := &pubsubClientMock{
		createTopic: func(_ context.Context, topic *pubsubpb.Topic) error {
			createdTopic = topic.GetName()
			return nil
		},
		publisher: func(topicNameOrID string) publisherAPI {
			requestedPublisher = topicNameOrID
			return &publisherMock{}
		},
	}

	sink, err := newPubsubSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.NotNil(t, sink)
	require.Equal(t, "projects/my-project/topics/my-topic", createdTopic)
	require.Equal(t, "projects/my-project/topics/my-topic", requestedPublisher)
}

func TestNewPubsubSinkWithClientSkipsTopicCreationWhenDisabled(t *testing.T) {
	cfg := &PubsubConfig{GcloudProjectId: "my-project", Topic: "my-topic", CreateTopic: false}
	createCalled := false
	client := &pubsubClientMock{
		createTopic: func(_ context.Context, _ *pubsubpb.Topic) error {
			createCalled = true
			return nil
		},
		publisher: func(_ string) publisherAPI {
			return &publisherMock{}
		},
	}

	_, err := newPubsubSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.False(t, createCalled)
}

func TestNewPubsubSinkWithClientCreateTopicErrorClosesClient(t *testing.T) {
	cfg := &PubsubConfig{GcloudProjectId: "my-project", Topic: "my-topic", CreateTopic: true}
	createErr := errors.New("create topic failed")
	closed := false
	client := &pubsubClientMock{
		createTopic: func(_ context.Context, _ *pubsubpb.Topic) error {
			return createErr
		},
		close: func() error {
			closed = true
			return nil
		},
	}

	_, err := newPubsubSinkWithClient(cfg, client)
	require.ErrorIs(t, err, createErr)
	require.True(t, closed)
}

func TestPubsubSinkSendPublishesMessage(t *testing.T) {
	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	publishedData := []byte(nil)
	sink := &PubsubSink{
		publisher: &publisherMock{
			publish: func(_ context.Context, msg *pubsub.Message) publishResultAPI {
				publishedData = msg.Data
				return &publishResultMock{id: "message-id"}
			},
		},
	}

	err := sink.Send(context.Background(), ev)
	require.NoError(t, err)
	require.Equal(t, string(ev.ToJSON()), string(publishedData))
}

func TestPubsubSinkSendPropagatesPublishError(t *testing.T) {
	publishErr := errors.New("publish failed")
	sink := &PubsubSink{
		publisher: &publisherMock{
			publish: func(_ context.Context, _ *pubsub.Message) publishResultAPI {
				return &publishResultMock{err: publishErr}
			},
		},
	}

	err := sink.Send(context.Background(), &kube.EnhancedEvent{})
	require.ErrorIs(t, err, publishErr)
}

func TestPubsubSinkCloseStopsPublisherAndClosesClient(t *testing.T) {
	stopped := false
	closed := false
	sink := &PubsubSink{
		publisher: &publisherMock{stop: func() { stopped = true }},
		pubsubClient: &pubsubClientMock{close: func() error {
			closed = true
			return nil
		}},
	}

	sink.Close()
	require.True(t, stopped)
	require.True(t, closed)
}
