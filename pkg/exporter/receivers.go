package exporter

import "context"

import (
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/sinks"
)

type Delivery struct {
	Ctx   context.Context
	Event kube.EnhancedEvent
}

// ReceiverRegistry registers a receiver with the appropriate sink
type ReceiverRegistry interface {
	SendEvent(context.Context, string, *kube.EnhancedEvent)
	Register(string, sinks.Sink)
	Close()
}
