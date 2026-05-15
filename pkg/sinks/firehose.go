package sinks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	firehosetypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
)

type FirehoseConfig struct {
	Layout             map[string]any `yaml:"layout"`
	DeliveryStreamName string         `yaml:"deliveryStreamName"`
	Region             string         `yaml:"region"`
	Endpoint           string         `yaml:"endpoint"`
	// DeDot all labels and annotations in the event. For both the event and the involvedObject
	DeDot bool `yaml:"deDot"`
}

type FirehoseSink struct {
	cfg *FirehoseConfig
	svc firehoseAPI
}

func NewFirehoseSink(cfg *FirehoseConfig) (Sink, error) {
	ctx := context.Background()
	svc, err := buildFirehoseClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return newFirehoseSinkWithClient(cfg, svc)
}

type firehoseAPI interface {
	PutRecord(ctx context.Context, params *firehose.PutRecordInput, optFns ...func(*firehose.Options)) (*firehose.PutRecordOutput, error)
}

func buildFirehoseClient(ctx context.Context, cfg *FirehoseConfig) (firehoseAPI, error) {
	if cfg == nil {
		return nil, fmt.Errorf("firehose config is nil")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}

	if cfg.Endpoint != "" {
		return firehose.NewFromConfig(awsCfg, func(options *firehose.Options) {
			options.Region = cfg.Region
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.RetryMode = aws.RetryModeAdaptive
			options.RetryMaxAttempts = 3
		}), nil
	}

	return firehose.NewFromConfig(awsCfg), nil
}

func newFirehoseSinkWithClient(cfg *FirehoseConfig, svc firehoseAPI) (Sink, error) {
	if cfg == nil {
		return nil, fmt.Errorf("firehose config is nil")
	}
	if svc == nil {
		return nil, fmt.Errorf("firehose client is nil")
	}

	return &FirehoseSink{
		cfg: cfg,
		svc: svc,
	}, nil
}

func (f *FirehoseSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	var toSend []byte

	if f.cfg.DeDot {
		de := ev.DeDot()
		ev = &de
	}

	if f.cfg.Layout != nil {
		res, err := convertLayoutTemplate(f.cfg.Layout, ev)
		if err != nil {
			return err
		}

		toSend, err = json.Marshal(res)
		if err != nil {
			return err
		}
	} else {
		toSend = ev.ToJSON()
	}

	_, err := f.svc.PutRecord(ctx, &firehose.PutRecordInput{
		Record: &firehosetypes.Record{
			Data: toSend,
		},
		DeliveryStreamName: aws.String(f.cfg.DeliveryStreamName),
	})

	return err
}

func (f *FirehoseSink) Close() {
	// No-op
}
