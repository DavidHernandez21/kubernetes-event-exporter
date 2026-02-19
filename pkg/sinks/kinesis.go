package sinks

import (
	"context"
	"encoding/json"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
)

type KinesisConfig struct {
	Layout     map[string]any `yaml:"layout"`
	StreamName string         `yaml:"streamName"`
	Region     string         `yaml:"region"`
}

type KinesisSink struct {
	cfg *KinesisConfig
	svc *kinesis.Kinesis
}

func NewKinesisSink(cfg *KinesisConfig) (Sink, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: new(cfg.Region)},
	)
	if err != nil {
		return nil, err
	}

	return &KinesisSink{
		cfg: cfg,
		svc: kinesis.New(sess),
	}, nil
}

func (k *KinesisSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	var toSend []byte

	if k.cfg.Layout != nil {
		res, err := convertLayoutTemplate(k.cfg.Layout, ev)
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

	_, err := k.svc.PutRecord(&kinesis.PutRecordInput{
		Data:         toSend,
		PartitionKey: new(string(ev.UID)),
		StreamName:   new(k.cfg.StreamName),
	})

	return err
}

func (k *KinesisSink) Close() {
	// No-op
}
