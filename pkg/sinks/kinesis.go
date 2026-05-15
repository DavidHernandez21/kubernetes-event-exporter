package sinks

import (
	"context"
	"fmt"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

type KinesisConfig struct {
	Layout     map[string]any `yaml:"layout"`
	StreamName string         `yaml:"streamName"`
	Region     string         `yaml:"region"`
	Endpoint   string         `yaml:"endpoint"`
}

type KinesisSink struct {
	cfg *KinesisConfig
	svc kinesisAPI
}

func NewKinesisSink(cfg *KinesisConfig) (Sink, error) {
	ctx := context.Background()
	svc, err := buildKinesisClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return newKinesisSinkWithClient(cfg, svc)
}

type kinesisAPI interface {
	PutRecord(ctx context.Context, params *kinesis.PutRecordInput, optFns ...func(*kinesis.Options)) (*kinesis.PutRecordOutput, error)
}

func buildKinesisClient(ctx context.Context, cfg *KinesisConfig) (kinesisAPI, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kinesis config is nil")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}

	if cfg.Endpoint != "" {
		return kinesis.NewFromConfig(awsCfg, func(options *kinesis.Options) {
			options.Region = cfg.Region
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.RetryMode = aws.RetryModeAdaptive
			options.RetryMaxAttempts = 3
		}), nil
	}

	return kinesis.NewFromConfig(awsCfg), nil
}

func newKinesisSinkWithClient(cfg *KinesisConfig, svc kinesisAPI) (Sink, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kinesis config is nil")
	}
	if svc == nil {
		return nil, fmt.Errorf("kinesis client is nil")
	}

	return &KinesisSink{
		cfg: cfg,
		svc: svc,
	}, nil
}

func (k *KinesisSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	toSend, err := serializeEventWithLayout(k.cfg.Layout, ev)
	if err != nil {
		return err
	}

	_, err = k.svc.PutRecord(ctx, &kinesis.PutRecordInput{
		Data:         toSend,
		PartitionKey: aws.String(string(ev.UID)),
		StreamName:   aws.String(k.cfg.StreamName),
	})

	return err
}

func (k *KinesisSink) Close() {
	// No-op
}
