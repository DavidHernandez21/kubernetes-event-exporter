//go:build integration
// +build integration

package sinks

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
)

func TestSNSSinkLocalStack(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1")
	container, err := localstack.Run(
		ctx,
		"nahuelnucera/ministack:latest",
		testcontainers.WithEnv(map[string]string{
			"MINISTACK_HOST":       "127.0.0.1",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/_ministack/health").WithPort("4566/tcp").WithStatusCodeMatcher(func(code int) bool {
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
	topicName := "kube-events"
	queueName := "kube-events-sub"

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	snsClient := sns.NewFromConfig(awsCfg, func(options *sns.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})
	sqsClient := sqs.NewFromConfig(awsCfg, func(options *sqs.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})

	topicOut, err := snsClient.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(topicName)})
	require.NoError(t, err)
	queueOut, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(queueName)})
	require.NoError(t, err)

	queueURL := replaceURLHost(aws.ToString(queueOut.QueueUrl), endpointHost)
	attrsOut, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)
	queueARN := attrsOut.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
	require.NotEmpty(t, queueARN)

	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Sid":"AllowSnsPublish","Effect":"Allow","Principal":"*","Action":"sqs:SendMessage","Resource":"%s","Condition":{"ArnEquals":{"aws:SourceArn":"%s"}}}]}`,
		queueARN,
		aws.ToString(topicOut.TopicArn),
	)
	_, err = sqsClient.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(queueURL),
		Attributes: map[string]string{
			"Policy": policy,
		},
	})
	require.NoError(t, err)

	subOut, err := snsClient.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn: topicOut.TopicArn,
		Protocol: aws.String("sqs"),
		Endpoint: aws.String(queueARN),
		Attributes: map[string]string{
			"RawMessageDelivery": "true",
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, aws.ToString(subOut.SubscriptionArn))

	sink, err := NewSNSSink(&SNSConfig{
		TopicARN: aws.ToString(topicOut.TopicArn),
		Region:   region,
		Endpoint: endpoint,
	})
	require.NoError(t, err)

	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	require.NoError(t, sink.Send(ctx, ev))

	recvOut, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     5,
	})
	require.NoError(t, err)
	require.Len(t, recvOut.Messages, 1)

	received := aws.ToString(recvOut.Messages[0].Body)
	// LocalStack may still wrap SNS payloads even with RawMessageDelivery enabled.
	require.Equal(t, string(ev.ToJSON()), extractSNSMessage(received))
}

func extractSNSMessage(body string) string {
	var envelope struct {
		Type     string `json:"Type"`
		TopicArn string `json:"TopicArn"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err == nil && envelope.Type == "Notification" && envelope.TopicArn != "" {
		return envelope.Message
	}
	return body
}
