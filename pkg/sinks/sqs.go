package sinks

import (
	"context"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type SQSConfig struct {
	Layout    map[string]any `yaml:"layout"`
	QueueName string         `yaml:"queueName"`
	Region    string         `yaml:"region"`
}

type SQSSink struct {
	cfg      *SQSConfig
	svc      *sqs.SQS
	queueURL string
}

func NewSQSSink(cfg *SQSConfig) (Sink, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: new(cfg.Region)},
	)
	if err != nil {
		return nil, err
	}

	svc := sqs.New(sess)
	out, err := svc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: &cfg.QueueName,
	})

	if err != nil {
		return nil, err
	}

	return &SQSSink{
		cfg:      cfg,
		svc:      svc,
		queueURL: *out.QueueUrl,
	}, nil
}

func (s *SQSSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	toSend, e := serializeEventWithLayout(s.cfg.Layout, ev)
	if e != nil {
		return e
	}

	_, err := s.svc.SendMessageWithContext(ctx, &sqs.SendMessageInput{
		MessageBody: new(string(toSend)),
		QueueUrl:    &s.queueURL,
	})

	return err
}

func (s *SQSSink) Close() {
	// No-op
}
