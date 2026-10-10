package exporter

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type channelRegistrySinkStub struct {
	sent   *kube.EnhancedEvent
	ctx    context.Context
	closed bool
}

func (s *channelRegistrySinkStub) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	s.ctx = ctx
	s.sent = evClone(ev)
	return nil
}

func (s *channelRegistrySinkStub) Close() {
	s.closed = true
}

type blockingChannelRegistrySinkStub struct {
	release chan struct{}
	sent    []*kube.EnhancedEvent
}

func (s *blockingChannelRegistrySinkStub) Send(_ context.Context, event *kube.EnhancedEvent) error {
	<-s.release
	s.sent = append(s.sent, evClone(event))
	return nil
}

func (s *blockingChannelRegistrySinkStub) Close() {}

func evClone(ev *kube.EnhancedEvent) *kube.EnhancedEvent {
	clone := *ev
	return &clone
}

func TestChannelBasedReceiverRegistrySendAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := &ChannelBasedReceiverRegistry{}
		sink := &channelRegistrySinkStub{}

		registry.Register("sink", sink, ReceiverOptions{})
		expected := &kube.EnhancedEvent{Message: "hello"}
		registry.SendEvent(context.Background(), "sink", expected)
		synctest.Wait()

		require.NotNil(t, sink.sent)
		require.Equal(t, *expected, *sink.sent)

		registry.Close()
		require.True(t, sink.closed)
	})
}

func TestChannelBasedReceiverRegistryPreservesContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := &ChannelBasedReceiverRegistry{}
		sink := &channelRegistrySinkStub{}
		registry.Register("sink", sink, ReceiverOptions{})

		ctx := context.WithValue(context.Background(), "trace-test", "present")
		registry.SendEvent(ctx, "sink", &kube.EnhancedEvent{Message: "hello"})
		synctest.Wait()

		require.Equal(t, "present", sink.ctx.Value("trace-test"))
		registry.Close()
	})
}

func TestChannelBasedReceiverRegistryDropsEventWhenBoundedQueueIsFull(t *testing.T) {
	testSpanExporter.Reset()

	synctest.Test(t, func(t *testing.T) {
		metricsStore := metrics.NewMetricsStore("channel_registry_test_")
		defer metrics.DestroyMetricsStore(metricsStore)

		registry := &ChannelBasedReceiverRegistry{MetricsStore: metricsStore}
		sink := &blockingChannelRegistrySinkStub{release: make(chan struct{})}
		registry.Register("slow", sink, ReceiverOptions{EnableBoundedQueue: true, QueueCapacity: 1})

		registry.SendEvent(context.Background(), "slow", &kube.EnhancedEvent{Message: "first"})
		synctest.Wait()

		registry.SendEvent(context.Background(), "slow", &kube.EnhancedEvent{Message: "second"})
		registry.SendEvent(context.Background(), "slow", &kube.EnhancedEvent{Message: "dropped"})

		require.Equal(t, float64(1), testutil.ToFloat64(metricsStore.ReceiverQueueDrops.WithLabelValues("slow")))

		close(sink.release)
		synctest.Wait()
		registry.Close()

		require.Len(t, sink.sent, 2)
		require.Equal(t, "first", sink.sent[0].Message)
		require.Equal(t, "second", sink.sent[1].Message)
	})

	var foundDropEvent bool
	for _, span := range testSpanExporter.GetSpans() {
		if span.Name != "kubernetes.event.dispatch" {
			continue
		}
		for _, spanEvent := range span.Events {
			if spanEvent.Name != "receiver.queue.enqueue" {
				continue
			}

			attributes := make(map[string]any, len(spanEvent.Attributes))
			for _, attribute := range spanEvent.Attributes {
				attributes[string(attribute.Key)] = attribute.Value.AsInterface()
			}
			if attributes["k8s.event.queue.outcome"] != "dropped" {
				continue
			}

			foundDropEvent = true
			require.Equal(t, "slow", attributes["k8s.event.receiver"])
			require.EqualValues(t, 1, attributes["k8s.event.queue.capacity"])
			require.EqualValues(t, 1, attributes["k8s.event.queue.length"])
		}
	}
	require.True(t, foundDropEvent)
}

func TestChannelBasedReceiverRegistryKeepsReceiversIsolatedWhenQueueIsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := &ChannelBasedReceiverRegistry{}
		slowSink := &blockingChannelRegistrySinkStub{release: make(chan struct{})}
		fastSink := &channelRegistrySinkStub{}
		options := ReceiverOptions{EnableBoundedQueue: true, QueueCapacity: 1}
		registry.Register("slow", slowSink, options)
		registry.Register("fast", fastSink, options)

		registry.SendEvent(context.Background(), "slow", &kube.EnhancedEvent{Message: "first"})
		synctest.Wait()
		registry.SendEvent(context.Background(), "slow", &kube.EnhancedEvent{Message: "second"})
		registry.SendEvent(context.Background(), "slow", &kube.EnhancedEvent{Message: "dropped"})

		registry.SendEvent(context.Background(), "fast", &kube.EnhancedEvent{Message: "delivered"})
		synctest.Wait()
		require.NotNil(t, fastSink.sent)
		require.Equal(t, "delivered", fastSink.sent.Message)

		close(slowSink.release)
		synctest.Wait()
		registry.Close()
	})
}
