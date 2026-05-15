package sinks

import (
	"context"
	"fmt"
	"strconv"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// OpsCenterConfig is the configuration of the Sink.
type OpsCenterConfig struct {
	OperationalData map[string]string `yaml:"operationalData"`
	Tags            map[string]string `yaml:"tags"`
	Category        string            `yaml:"category"`
	Description     string            `yaml:"description"`
	Priority        string            `yaml:"priority"`
	Region          string            `yaml:"region"`
	Severity        string            `yaml:"severity"`
	Source          string            `yaml:"source"`
	Title           string            `yaml:"title"`
	Notifications   []string          `yaml:"notifications"`
	RelatedOpsItems []string          `yaml:"relatedOpsItems"`
	Endpoint        string            `yaml:"endpoint"`
}

// OpsCenterSink is an AWS OpsCenter notifcation path.
type OpsCenterSink struct {
	cfg *OpsCenterConfig
	svc opsCenterAPI
}

// NewOpsCenterSink returns a new OpsCenterSink.
func NewOpsCenterSink(cfg *OpsCenterConfig) (Sink, error) {
	ctx := context.Background()
	svc, err := buildOpsCenterClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return newOpsCenterSinkWithClient(cfg, svc)
}

type opsCenterAPI interface {
	CreateOpsItem(ctx context.Context, params *ssm.CreateOpsItemInput, optFns ...func(*ssm.Options)) (*ssm.CreateOpsItemOutput, error)
}

func buildOpsCenterClient(ctx context.Context, cfg *OpsCenterConfig) (opsCenterAPI, error) {
	if cfg == nil {
		return nil, fmt.Errorf("opscenter config is nil")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}

	if cfg.Endpoint != "" {
		return ssm.NewFromConfig(awsCfg, func(options *ssm.Options) {
			options.Region = cfg.Region
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.RetryMode = aws.RetryModeAdaptive
			options.RetryMaxAttempts = 3
		}), nil
	}

	return ssm.NewFromConfig(awsCfg), nil
}

func newOpsCenterSinkWithClient(cfg *OpsCenterConfig, svc opsCenterAPI) (Sink, error) {
	if cfg == nil {
		return nil, fmt.Errorf("opscenter config is nil")
	}
	if svc == nil {
		return nil, fmt.Errorf("opscenter client is nil")
	}

	return &OpsCenterSink{
		cfg: cfg,
		svc: svc,
	}, nil
}

// Send ...
func (s *OpsCenterSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	oi := ssm.CreateOpsItemInput{}
	t, err := GetString(ev, s.cfg.Title)
	if err != nil {
		return err
	}
	oi.Title = aws.String(t)
	d, err := GetString(ev, s.cfg.Description)
	if err != nil {
		return err
	}
	oi.Description = aws.String(d)
	su, err := GetString(ev, s.cfg.Source)
	if err != nil {
		return err
	}
	oi.Source = aws.String(su)

	// Category is optional although highly recommended
	if len(s.cfg.Category) != 0 {
		c, err := GetString(ev, s.cfg.Category)
		if err != nil {
			return err
		}
		oi.Category = aws.String(c)
	}

	// Severity is optional although highly recommended
	if len(s.cfg.Severity) != 0 {
		se, err := GetString(ev, s.cfg.Severity)
		if err != nil {
			return err
		}
		oi.Severity = aws.String(se)
	}

	// Priority is optional although highly recommended
	if len(s.cfg.Priority) != 0 {
		p, err := GetString(ev, s.cfg.Priority)
		if err != nil {
			return err
		}
		n, err := strconv.ParseInt(p, 10, 32)
		if err != nil {
			return fmt.Errorf("Priority is a non int")
		}
		pn := int32(n)
		oi.Priority = &pn
	}
	if s.cfg.OperationalData != nil {
		oids := make(map[string]ssmtypes.OpsItemDataValue)
		for k, v := range s.cfg.OperationalData {
			dv, err := GetString(ev, v)
			if err != nil {
				return err
			}
			oids[k] = ssmtypes.OpsItemDataValue{Type: ssmtypes.OpsItemDataTypeSearchableString, Value: aws.String(dv)}
		}
		oi.OperationalData = oids
	}
	if s.cfg.Tags != nil {
		tvs := make([]ssmtypes.Tag, 0)
		for k, v := range s.cfg.Tags {
			tv, err := GetString(ev, v)
			if err != nil {
				return err
			}
			tvs = append(tvs, ssmtypes.Tag{Key: aws.String(k), Value: aws.String(tv)})
		}
		oi.Tags = tvs
	}
	if s.cfg.RelatedOpsItems != nil {
		ris := make([]ssmtypes.RelatedOpsItem, 0)
		for _, v := range s.cfg.RelatedOpsItems {
			ri, err := GetString(ev, v)
			if err != nil {
				return err
			}
			ris = append(ris, ssmtypes.RelatedOpsItem{OpsItemId: aws.String(ri)})
		}
		oi.RelatedOpsItems = ris
	}
	if s.cfg.Notifications != nil {
		ns := make([]ssmtypes.OpsItemNotification, 0)
		for _, v := range s.cfg.Notifications {
			n, err := GetString(ev, v)
			if err != nil {
				return err
			}
			ns = append(ns, ssmtypes.OpsItemNotification{Arn: aws.String(n)})
		}
		oi.Notifications = ns
	}

	_, createErr := s.svc.CreateOpsItem(ctx, &oi)

	return createErr
}

// Close ...
func (s *OpsCenterSink) Close() {
	// No-op
}
