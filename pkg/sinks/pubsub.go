package sinks

import (
	"context"
	"fmt"

	pubsub "cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/rs/zerolog/log"
)

// PubsubConfig contains receiver settings for the Google Cloud Pub/Sub sink.
type PubsubConfig struct {
	GcloudProjectId string `yaml:"gcloud_project_id"`
	Topic           string `yaml:"topic"`
	CreateTopic     bool   `yaml:"create_topic"`
}

// PubsubSink sends Kubernetes events to a Pub/Sub topic using the v2 client.
type PubsubSink struct {
	cfg          *PubsubConfig
	pubsubClient pubsubClientAPI
	publisher    publisherAPI
}

// publishResultAPI is a minimal abstraction over publish result retrieval.
type publishResultAPI interface {
	Get(ctx context.Context) (serverID string, err error)
}

// publisherAPI captures publish operations used by the sink.
type publisherAPI interface {
	Publish(ctx context.Context, msg *pubsub.Message) publishResultAPI
	Stop()
}

// pubsubClientAPI captures client operations required by the sink.
type pubsubClientAPI interface {
	CreateTopic(ctx context.Context, topic *pubsubpb.Topic) error
	Publisher(topicNameOrID string) publisherAPI
	Close() error
}

// realPubsubClient adapts the concrete v2 client to pubsubClientAPI.
type realPubsubClient struct {
	client *pubsub.Client
}

// CreateTopic creates a topic via the v2 topic admin client.
func (c *realPubsubClient) CreateTopic(ctx context.Context, topic *pubsubpb.Topic) error {
	_, err := c.client.TopicAdminClient.CreateTopic(ctx, topic)
	return err
}

// Publisher returns a publisher bound to the given topic name or ID.
func (c *realPubsubClient) Publisher(topicNameOrID string) publisherAPI {
	return &realPublisher{publisher: c.client.Publisher(topicNameOrID)}
}

// Close releases resources held by the underlying Pub/Sub client.
func (c *realPubsubClient) Close() error {
	return c.client.Close()
}

// realPublisher adapts the concrete v2 publisher to publisherAPI.
type realPublisher struct {
	publisher *pubsub.Publisher
}

// Publish enqueues a message for asynchronous publishing.
func (p *realPublisher) Publish(ctx context.Context, msg *pubsub.Message) publishResultAPI {
	return p.publisher.Publish(ctx, msg)
}

// Stop flushes and stops background publisher goroutines.
func (p *realPublisher) Stop() {
	p.publisher.Stop()
}

// NewPubsubSink builds a Pub/Sub sink backed by the v2 client.
func NewPubsubSink(cfg *PubsubConfig) (Sink, error) {
	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, cfg.GcloudProjectId) // TODO: add options here
	if err != nil {
		return nil, err
	}

	return newPubsubSinkWithClient(cfg, &realPubsubClient{client: client})
}

// newPubsubSinkWithClient constructs a sink with an injected client for testing.
func newPubsubSinkWithClient(cfg *PubsubConfig, client pubsubClientAPI) (Sink, error) {
	if cfg == nil {
		return nil, fmt.Errorf("pubsub config is nil")
	}
	if client == nil {
		return nil, fmt.Errorf("pubsub client is nil")
	}

	ctx := context.Background()
	topicName := fmt.Sprintf("projects/%s/topics/%s", cfg.GcloudProjectId, cfg.Topic)

	if cfg.CreateTopic {
		err := client.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName})
		if err != nil {
			_ = client.Close()
			return nil, err
		}
		log.Info().Msgf("pubsub: created topic: %s", cfg.Topic)
	}
	publisher := client.Publisher(topicName)

	return &PubsubSink{
		pubsubClient: client,
		publisher:    publisher,
		cfg:          cfg,
	}, nil
}

// Send publishes a single enhanced Kubernetes event to the configured topic.
func (ps *PubsubSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	msg := &pubsub.Message{
		Data: ev.ToJSON(),
	}
	_, err := ps.publisher.Publish(ctx, msg).Get(ctx)
	return err
}

// Close stops the publisher and closes the underlying Pub/Sub client.
func (ps *PubsubSink) Close() {
	log.Info().Msgf("pubsub: Closing topic...")
	ps.publisher.Stop()
	if err := ps.pubsubClient.Close(); err != nil {
		log.Error().Err(err).Msg("pubsub: failed to close client")
	}
}
