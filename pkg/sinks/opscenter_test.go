package sinks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type opsCenterClientMock struct {
	createOpsItem func(ctx context.Context, input *ssm.CreateOpsItemInput) (*ssm.CreateOpsItemOutput, error)
}

func (m *opsCenterClientMock) CreateOpsItem(ctx context.Context, input *ssm.CreateOpsItemInput, _ ...func(*ssm.Options)) (*ssm.CreateOpsItemOutput, error) {
	if m.createOpsItem == nil {
		return nil, errors.New("create ops item not implemented")
	}
	return m.createOpsItem(ctx, input)
}

func TestOpsCenterSinkSendCreatesOpsItem(t *testing.T) {
	ev := &kube.EnhancedEvent{}
	ev.Namespace = "default"
	ev.Reason = "my reason"
	ev.Type = "Warning"
	ev.InvolvedObject.Kind = "Pod"
	ev.InvolvedObject.Name = "nginx-server-123abc-456def"
	ev.InvolvedObject.Namespace = "prod"
	ev.Message = "Successfully pulled image \"nginx:latest\""
	ev.FirstTimestamp = v1.Time{Time: time.Now()}

	cfg := &OpsCenterConfig{
		Title:           "{{ .Message }}",
		Category:        "{{ .Reason }}",
		Description:     "Event {{ .Reason }} for {{ .InvolvedObject.Namespace }}/{{ .InvolvedObject.Name }} on K8s cluster",
		Notifications:   []string{"sns1", "sns2"},
		OperationalData: map[string]string{"Reason": "{{ .Reason }}"},
		Priority:        "6",
		Region:          "us-east1",
		RelatedOpsItems: []string{"ops1", "ops2"},
		Severity:        "6",
		Source:          "production",
		Tags:            map[string]string{"ENV": "{{ .InvolvedObject.Namespace }}"},
	}

	client := &opsCenterClientMock{
		createOpsItem: func(_ context.Context, input *ssm.CreateOpsItemInput) (*ssm.CreateOpsItemOutput, error) {
			require.Equal(t, "Successfully pulled image \"nginx:latest\"", aws.ToString(input.Title))
			require.Equal(t, "Event my reason for prod/nginx-server-123abc-456def on K8s cluster", aws.ToString(input.Description))
			require.Equal(t, "production", aws.ToString(input.Source))
			require.Equal(t, "my reason", aws.ToString(input.Category))
			require.Equal(t, "6", aws.ToString(input.Severity))
			require.EqualValues(t, 6, aws.ToInt32(input.Priority))

			require.Len(t, input.Notifications, 2)
			require.Equal(t, "sns1", aws.ToString(input.Notifications[0].Arn))
			require.Equal(t, "sns2", aws.ToString(input.Notifications[1].Arn))

			require.Contains(t, input.OperationalData, "Reason")
			require.Equal(t, ssmtypes.OpsItemDataTypeSearchableString, input.OperationalData["Reason"].Type)
			require.Equal(t, "my reason", aws.ToString(input.OperationalData["Reason"].Value))

			require.Len(t, input.RelatedOpsItems, 2)
			require.Equal(t, "ops1", aws.ToString(input.RelatedOpsItems[0].OpsItemId))
			require.Equal(t, "ops2", aws.ToString(input.RelatedOpsItems[1].OpsItemId))

			require.Len(t, input.Tags, 1)
			require.Equal(t, "ENV", aws.ToString(input.Tags[0].Key))
			require.Equal(t, "prod", aws.ToString(input.Tags[0].Value))
			return &ssm.CreateOpsItemOutput{OpsItemId: aws.String("id123456")}, nil
		},
	}

	sink, err := newOpsCenterSinkWithClient(cfg, client)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), ev))
}

func TestOpsCenterSinkSendInvalidPriorityReturnsError(t *testing.T) {
	cfg := &OpsCenterConfig{
		Title:       "{{ .Message }}",
		Description: "{{ .Message }}",
		Priority:    "asdf",
		Region:      "us-east1",
		Source:      "production",
	}
	client := &opsCenterClientMock{}

	sink, err := newOpsCenterSinkWithClient(cfg, client)
	require.NoError(t, err)
	err = sink.Send(context.Background(), &kube.EnhancedEvent{})
	require.Error(t, err)
}
