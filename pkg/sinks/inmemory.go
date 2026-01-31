package sinks

import (
	"context"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
)

type InMemoryConfig struct {
	Ref *InMemory
}

type InMemory struct {
	Config *InMemoryConfig
	Events []*kube.EnhancedEvent
}

func (i *InMemory) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	i.Events = append(i.Events, ev)
	return nil
}

func (i *InMemory) Close() {
	// No-op
}
