package sinks

import (
	"context"
	"fmt"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type SQSConfig struct {
	Layout    map[string]any `yaml:"layout"`
	QueueName string         `yaml:"queueName"`
	Region    string         `yaml:"region"`
	Endpoint  string         `yaml:"endpoint"`
}

type SQSSink struct {
	cfg      *SQSConfig
	svc      sqsAPI
	queueURL string
}

type sqsAPI interface {
	GetQueueUrl(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

func NewSQSSink(cfg *SQSConfig) (Sink, error) {
	ctx := context.Background()
	svc, err := buildSQSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return newSQSSinkWithClient(ctx, cfg, svc)
}

func buildSQSClient(ctx context.Context, cfg *SQSConfig) (sqsAPI, error) {
	if cfg == nil {
		return nil, fmt.Errorf("sqs config is nil")
	}

	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	// Endpoint override is applied at the service client level to avoid deprecated global resolvers.

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}

	if cfg.Endpoint != "" {
		return sqs.NewFromConfig(awsCfg, func(options *sqs.Options) {
			options.Region = cfg.Region
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.RetryMode = aws.RetryModeAdaptive
			options.RetryMaxAttempts = 3
		}), nil
	}

	return sqs.NewFromConfig(awsCfg), nil
}

func newSQSSinkWithClient(ctx context.Context, cfg *SQSConfig, svc sqsAPI) (Sink, error) {
	out, err := svc.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(cfg.QueueName),
	})
	if err != nil {
		return nil, err
	}

	return &SQSSink{
		cfg:      cfg,
		svc:      svc,
		queueURL: aws.ToString(out.QueueUrl),
	}, nil
}

func (s *SQSSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	toSend, e := serializeEventWithLayout(s.cfg.Layout, ev)
	if e != nil {
		return e
	}

	_, err := s.svc.SendMessage(ctx, &sqs.SendMessageInput{
		MessageBody: aws.String(string(toSend)),
		QueueUrl:    aws.String(s.queueURL),
	})

	return err
}

func (s *SQSSink) Close() {
	// No-op
}
