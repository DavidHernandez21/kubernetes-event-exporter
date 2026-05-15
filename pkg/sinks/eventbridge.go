package sinks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/rs/zerolog/log"
)

type EventBridgeConfig struct {
	DetailType   string         `yaml:"detailType"`
	Details      map[string]any `yaml:"details"`
	Source       string         `yaml:"source"`
	EventBusName string         `yaml:"eventBusName"`
	Region       string         `yaml:"region"`
	Endpoint     string         `yaml:"endpoint"`
}

type EventBridgeSink struct {
	cfg *EventBridgeConfig
	svc eventbridgeAPI
}

func NewEventBridgeSink(cfg *EventBridgeConfig) (Sink, error) {
	ctx := context.Background()
	svc, err := buildEventBridgeClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return newEventBridgeSinkWithClient(cfg, svc)
}

type eventbridgeAPI interface {
	PutEvents(ctx context.Context, params *eventbridge.PutEventsInput, optFns ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error)
}

func buildEventBridgeClient(ctx context.Context, cfg *EventBridgeConfig) (eventbridgeAPI, error) {
	if cfg == nil {
		return nil, fmt.Errorf("eventbridge config is nil")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}

	if cfg.Endpoint != "" {
		return eventbridge.NewFromConfig(awsCfg, func(options *eventbridge.Options) {
			options.Region = cfg.Region
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.RetryMode = aws.RetryModeAdaptive
			options.RetryMaxAttempts = 3
		}), nil
	}

	return eventbridge.NewFromConfig(awsCfg), nil
}

func newEventBridgeSinkWithClient(cfg *EventBridgeConfig, svc eventbridgeAPI) (Sink, error) {
	if cfg == nil {
		return nil, fmt.Errorf("eventbridge config is nil")
	}
	if svc == nil {
		return nil, fmt.Errorf("eventbridge client is nil")
	}

	return &EventBridgeSink{
		cfg: cfg,
		svc: svc,
	}, nil
}

func (s *EventBridgeSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	log.Info().Msg("Sending event to EventBridge ")
	var toSend string
	if s.cfg.Details != nil {
		res, err := convertLayoutTemplate(s.cfg.Details, ev)
		if err != nil {
			return err
		}

		b, err := json.Marshal(res)
		toSend = string(b)
		if err != nil {
			return err
		}
	} else {
		toSend = string(ev.ToJSON())
	}
	tym := time.Now()
	inputRequest := eventbridgetypes.PutEventsRequestEntry{
		Detail:       aws.String(toSend),
		DetailType:   aws.String(s.cfg.DetailType),
		Time:         &tym,
		Source:       aws.String(s.cfg.Source),
		EventBusName: aws.String(s.cfg.EventBusName),
	}
	log.Info().Str("InputEvent", toSend).Msg("Request")

	_, err := s.svc.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []eventbridgetypes.PutEventsRequestEntry{inputRequest}})
	if err != nil {
		log.Error().Err(err).Msg("EventBridge Error")
		return err
	}
	return nil
}

func (s *EventBridgeSink) Close() {
}
