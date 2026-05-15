//go:build integration
// +build integration

package sinks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
)

func TestOpsCenterSinkLocalStack(t *testing.T) {
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
	endpoint := fmt.Sprintf("http://%s:%s", normalizeHost(host), mappedPort.Port())
	region := "us-east-1"
	source := fmt.Sprintf("kube-exporter-%d", time.Now().UnixNano())

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	client := ssm.NewFromConfig(awsCfg, func(options *ssm.Options) {
		options.Region = region
		options.BaseEndpoint = aws.String(endpoint)
		options.RetryMode = aws.RetryModeAdaptive
		options.RetryMaxAttempts = 3
	})

	sink, err := NewOpsCenterSink(&OpsCenterConfig{
		Title:       "{{ .Message }}",
		Description: "{{ .Message }}",
		Source:      source,
		Region:      region,
		Endpoint:    endpoint,
	})
	require.NoError(t, err)

	ev := &kube.EnhancedEvent{Event: corev1.Event{Message: "hello from opscenter"}}
	err = sink.Send(ctx, ev)
	if err != nil {
		errText := err.Error()
		if strings.Contains(errText, "Unknown action: CreateOpsItem") || strings.Contains(errText, "InvalidAction") {
			t.Skipf("localstack image does not support SSM CreateOpsItem: %v", err)
		}
	}
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		out, err := client.DescribeOpsItems(ctx, &ssm.DescribeOpsItemsInput{
			OpsItemFilters: []ssmtypes.OpsItemFilter{
				{
					Key:      ssmtypes.OpsItemFilterKeySource,
					Operator: ssmtypes.OpsItemFilterOperatorEqual,
					Values:   []string{source},
				},
			},
		})
		if err != nil {
			return false
		}
		for _, item := range out.OpsItemSummaries {
			if aws.ToString(item.Source) == source && aws.ToString(item.Title) == "hello from opscenter" {
				return true
			}
		}
		return false
	}, 2*time.Minute, 2*time.Second)
}
