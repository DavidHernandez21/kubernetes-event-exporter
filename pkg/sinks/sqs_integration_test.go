//go:build integration
// +build integration

package sinks

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
)

func TestSQSSinkLocalStack(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1")
	container, err := localstack.Run(
		ctx,
		"localstack/localstack:4.14.0",
		testcontainers.WithEnv(map[string]string{
			"SERVICES":              "sqs",
			"EAGER_SERVICE_LOADING": "1",
			"LOCALSTACK_HOST":       "127.0.0.1",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/_localstack/health").WithPort("4566/tcp").WithStatusCodeMatcher(func(code int) bool {
				return code >= 200 && code < 300
			}).WithStartupTimeout(2*time.Minute),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testcontainers.TerminateContainer(container)
	})

	host, err := container.Host(ctx)
	require.NoError(t, err)
	mappedPort, err := container.MappedPort(ctx, "4566/tcp")
	require.NoError(t, err)
	endpointHost := normalizeHost(host)
	endpoint := fmt.Sprintf("http://%s:%s", endpointHost, mappedPort.Port())
	region := "us-east-1"
	queueName := "kube-events"

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	client := sqs.NewFromConfig(awsCfg, func(options *sqs.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})
	createOut, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(queueName)})
	require.NoError(t, err)

	sink, err := NewSQSSink(&SQSConfig{
		QueueName: queueName,
		Region:    region,
		Endpoint:  endpoint,
	})
	require.NoError(t, err)

	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	require.NoError(t, sink.Send(ctx, ev))

	queueURL := replaceURLHost(aws.ToString(createOut.QueueUrl), endpointHost)
	recvOut, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     1,
	})
	require.NoError(t, err)
	require.Len(t, recvOut.Messages, 1)
	require.Equal(t, string(ev.ToJSON()), aws.ToString(recvOut.Messages[0].Body))
}

func normalizeHost(host string) string {
	if host == "localhost" {
		return "127.0.0.1"
	}
	return host
}

func replaceURLHost(rawURL, host string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	port := parsed.Port()
	if port != "" {
		parsed.Host = host + ":" + port
		return parsed.String()
	}
	parsed.Host = host
	return parsed.String()
}
