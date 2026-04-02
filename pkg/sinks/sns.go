package sinks

import (
	"context"
	"fmt"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type SNSConfig struct {
	Layout   map[string]any `yaml:"layout"`
	TopicARN string         `yaml:"topicARN"`
	Region   string         `yaml:"region"`
	Endpoint string         `yaml:"endpoint"`
}

type SNSSink struct {
	cfg *SNSConfig
	svc snsAPI
}

type snsAPI interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

func NewSNSSink(cfg *SNSConfig) (Sink, error) {
	ctx := context.Background()
	svc, err := buildSNSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return newSNSSinkWithClient(cfg, svc)
}

func buildSNSClient(ctx context.Context, cfg *SNSConfig) (snsAPI, error) {
	if cfg == nil {
		return nil, fmt.Errorf("sns config is nil")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}

	if cfg.Endpoint != "" {
		return sns.NewFromConfig(awsCfg, func(options *sns.Options) {
			options.Region = cfg.Region
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.RetryMode = aws.RetryModeAdaptive
			options.RetryMaxAttempts = 3
		}), nil
	}

	return sns.NewFromConfig(awsCfg), nil
}

func newSNSSinkWithClient(cfg *SNSConfig, svc snsAPI) (Sink, error) {
	if cfg == nil {
		return nil, fmt.Errorf("sns config is nil")
	}
	if svc == nil {
		return nil, fmt.Errorf("sns client is nil")
	}

	return &SNSSink{
		cfg: cfg,
		svc: svc,
	}, nil
}

func (s *SNSSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	toSend, e := serializeEventWithLayout(s.cfg.Layout, ev)
	if e != nil {
		return e
	}

	_, err := s.svc.Publish(ctx, &sns.PublishInput{
		Message:  aws.String(string(toSend)),
		TopicArn: aws.String(s.cfg.TopicARN),
	})

	return err
}

func (s *SNSSink) Close() {
	// No-op
}
