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
	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
)

func TestEventBridgeSinkLocalStack(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1")
	container, err := localstack.Run(
		ctx,
		"nahuelnucera/ministack:latest",
		testcontainers.WithEnv(map[string]string{
			"MINISTACK_HOST": "127.0.0.1",
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
	queueName := "kube-events-eventbridge"
	ruleName := "kube-events-rule"
	source := "cd"
	detailType := "deployment"
	eventBusName := "default"

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	eventbridgeClient := eventbridge.NewFromConfig(awsCfg, func(options *eventbridge.Options) {
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

	ruleOut, err := eventbridgeClient.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:         aws.String(ruleName),
		EventBusName: aws.String(eventBusName),
		EventPattern: aws.String(fmt.Sprintf(`{"source":[%q],"detail-type":[%q]}`, source, detailType)),
		State:        eventbridgetypes.RuleStateEnabled,
	})
	require.NoError(t, err)
	ruleARN := aws.ToString(ruleOut.RuleArn)
	require.NotEmpty(t, ruleARN)

	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Sid":"AllowEventBridgePublish","Effect":"Allow","Principal":{"Service":"events.amazonaws.com"},"Action":"sqs:SendMessage","Resource":"%s","Condition":{"ArnEquals":{"aws:SourceArn":"%s"}}}]}`,
		queueARN,
		ruleARN,
	)
	_, err = sqsClient.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(queueURL),
		Attributes: map[string]string{
			"Policy": policy,
		},
	})
	require.NoError(t, err)

	_, err = eventbridgeClient.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule:         aws.String(ruleName),
		EventBusName: aws.String(eventBusName),
		Targets: []eventbridgetypes.Target{{
			Id:  aws.String("queue-target"),
			Arn: aws.String(queueARN),
		}},
	})
	require.NoError(t, err)

	sink, err := NewEventBridgeSink(&EventBridgeConfig{
		DetailType:   detailType,
		Source:       source,
		EventBusName: eventBusName,
		Region:       region,
		Endpoint:     endpoint,
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

	var envelope struct {
		Source     string          `json:"source"`
		DetailType string          `json:"detail-type"`
		Detail     json.RawMessage `json:"detail"`
	}
	require.NoError(t, json.Unmarshal([]byte(aws.ToString(recvOut.Messages[0].Body)), &envelope))
	require.Equal(t, source, envelope.Source)
	require.Equal(t, detailType, envelope.DetailType)

	var detail map[string]any
	require.NoError(t, json.Unmarshal(envelope.Detail, &detail))
	require.Equal(t, "hello", detail["message"])
}
