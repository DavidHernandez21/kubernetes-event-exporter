package exporter

import (
	"context"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
)

type Router struct {
	cfg  *Config
	rcvr ReceiverRegistry
}

func (r *Router) ProcessEvent(event *kube.EnhancedEvent) {
	r.cfg.Route.ProcessEvent(context.Background(), event, r.rcvr)
}
