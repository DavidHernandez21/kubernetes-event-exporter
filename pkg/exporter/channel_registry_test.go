package exporter

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/stretchr/testify/require"
)

type channelRegistrySinkStub struct {
	sent   *kube.EnhancedEvent
	closed bool
}

func (s *channelRegistrySinkStub) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	s.sent = evClone(ev)
	return nil
}

func (s *channelRegistrySinkStub) Close() {
	s.closed = true
}

func evClone(ev *kube.EnhancedEvent) *kube.EnhancedEvent {
	clone := *ev
	return &clone
}

func TestChannelBasedReceiverRegistrySendAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := &ChannelBasedReceiverRegistry{}
		sink := &channelRegistrySinkStub{}

		registry.Register("sink", sink)
		expected := &kube.EnhancedEvent{Message: "hello"}
		registry.SendEvent("sink", expected)
		synctest.Wait()

		require.NotNil(t, sink.sent)
		require.Equal(t, *expected, *sink.sent)

		registry.Close()
		require.True(t, sink.closed)
	})
}
