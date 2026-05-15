//go:build integration
// +build integration

package sinks

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	firehosetypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
)

func TestFirehoseSinkLocalStack(t *testing.T) {
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
	deliveryStreamName := "kube-events-firehose"
	bucketName := "kube-events-firehose-bucket"
	roleARN := "arn:aws:iam::000000000000:role/firehose-delivery"

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	s3Client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})
	firehoseClient := firehose.NewFromConfig(awsCfg, func(options *firehose.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})

	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucketName)})
	require.NoError(t, err)

	_, err = firehoseClient.CreateDeliveryStream(ctx, &firehose.CreateDeliveryStreamInput{
		DeliveryStreamName: aws.String(deliveryStreamName),
		DeliveryStreamType: firehosetypes.DeliveryStreamTypeDirectPut,
		ExtendedS3DestinationConfiguration: &firehosetypes.ExtendedS3DestinationConfiguration{
			BucketARN:         aws.String(fmt.Sprintf("arn:aws:s3:::%s", bucketName)),
			RoleARN:           aws.String(roleARN),
			Prefix:            aws.String("firehose/"),
			ErrorOutputPrefix: aws.String("firehose-errors/"),
		},
	})
	require.NoError(t, err)

	require.NoError(t, waitForFirehoseStreamActive(ctx, firehoseClient, deliveryStreamName))

	sink, err := NewFirehoseSink(&FirehoseConfig{
		DeliveryStreamName: deliveryStreamName,
		Region:             region,
		Endpoint:           endpoint,
	})
	require.NoError(t, err)

	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello"}}
	require.NoError(t, sink.Send(ctx, ev))

	require.Eventually(t, func() bool {
		out, err := firehoseClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(deliveryStreamName),
		})
		if err != nil {
			return false
		}
		if out.DeliveryStreamDescription == nil {
			return false
		}
		return out.DeliveryStreamDescription.DeliveryStreamStatus == firehosetypes.DeliveryStreamStatusActive
	}, 2*time.Minute, 2*time.Second)
}

func waitForFirehoseStreamActive(ctx context.Context, client *firehose.Client, name string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := client.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(name),
		})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if out.DeliveryStreamDescription != nil && out.DeliveryStreamDescription.DeliveryStreamStatus == firehosetypes.DeliveryStreamStatusActive {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("firehose stream %s did not become active", name)
}
