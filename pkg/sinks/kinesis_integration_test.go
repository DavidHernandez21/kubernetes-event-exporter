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
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestKinesisSinkLocalStack(t *testing.T) {
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
	streamName := "kube-events-kinesis"

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	client := kinesis.NewFromConfig(awsCfg, func(options *kinesis.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})

	_, err = client.CreateStream(ctx, &kinesis.CreateStreamInput{
		StreamName: aws.String(streamName),
		ShardCount: aws.Int32(1),
	})
	require.NoError(t, err)

	require.NoError(t, waitForKinesisStreamActive(ctx, client, streamName))

	sink, err := NewKinesisSink(&KinesisConfig{
		StreamName: streamName,
		Region:     region,
		Endpoint:   endpoint,
	})
	require.NoError(t, err)

	ev := &kube.EnhancedEvent{Event: corev1.Event{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-1")},
		Message:    "hello",
	}}
	require.NoError(t, sink.Send(ctx, ev))

	descOut, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String(streamName)})
	require.NoError(t, err)
	require.NotNil(t, descOut.StreamDescription)
	require.NotEmpty(t, descOut.StreamDescription.Shards)
	shardID := aws.ToString(descOut.StreamDescription.Shards[0].ShardId)
	require.NotEmpty(t, shardID)

	iterOut, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String(streamName),
		ShardId:           aws.String(shardID),
		ShardIteratorType: kinesistypes.ShardIteratorTypeTrimHorizon,
	})
	require.NoError(t, err)
	require.NotEmpty(t, aws.ToString(iterOut.ShardIterator))

	require.Eventually(t, func() bool {
		recordsOut, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterOut.ShardIterator})
		if err != nil {
			return false
		}
		if len(recordsOut.Records) == 0 {
			return false
		}
		rec := recordsOut.Records[0]
		return aws.ToString(rec.PartitionKey) == "uid-1" && string(rec.Data) == string(ev.ToJSON())
	}, 2*time.Minute, 2*time.Second)
}

func waitForKinesisStreamActive(ctx context.Context, client *kinesis.Client, streamName string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := client.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{StreamName: aws.String(streamName)})
		if err == nil && out.StreamDescriptionSummary != nil && out.StreamDescriptionSummary.StreamStatus == kinesistypes.StreamStatusActive {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("kinesis stream %s did not become active", streamName)
}
