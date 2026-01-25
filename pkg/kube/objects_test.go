package kube

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/resmoio/kubernetes-event-exporter/pkg/metrics"
)

func newMetadataTestEnv(t *testing.T, ttl time.Duration) (*objectMetadataCache, *fake.Clientset, *dynfake.FakeDynamicClient, *corev1.ObjectReference) {
	t.Helper()

	provider := newObjectMetadataProviderWithTTL(1024, 256, ttl).(*objectMetadataCache)

	apiRes := &metav1.APIResourceList{
		GroupVersion: "apps/v1",
		APIResources: []metav1.APIResource{
			{
				Name:         "deployments",
				SingularName: "deployment",
				Namespaced:   true,
				Kind:         "Deployment",
			},
		},
	}

	cs := fake.NewClientset()
	if fd, ok := cs.Discovery().(*fakediscovery.FakeDiscovery); ok {
		fd.Resources = []*metav1.APIResourceList{apiRes}
	} else {
		t.Fatalf("expected fake discovery type")
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "test-deploy",
				"namespace": "default",
				"uid":       "test-uid",
				"labels": map[string]any{
					"test": "test",
				},
				"annotations": map[string]any{
					"test": "test",
				},
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "testAPI",
						"kind":       "testKind",
						"name":       "testOwner",
						"uid":        "testOwner",
					},
				},
			},
		},
	}

	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), u)

	ref := &corev1.ObjectReference{
		UID:             "test-uid",
		ResourceVersion: "1",
		APIVersion:      "apps/v1",
		Kind:            "Deployment",
		Name:            "test-deploy",
		Namespace:       "default",
	}

	return provider, cs, dyn, ref
}

func TestGetObjectMetadata_MappingCacheMissThenHit_WithFakeClients(t *testing.T) {
	metricsStore := metrics.NewMetricsStore("test_")
	defer metrics.DestroyMetricsStore(metricsStore)

	provider, cs, dyn, ref := newMetadataTestEnv(t, 12*time.Hour)

	// First call: mapping miss -> provider should call discovery+RESTMapping and then dyn Get
	meta, err := provider.getObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"test": "test"}, meta.Labels)
	// mapping should have been resolved and cached: assert mappingCache contains the mappingKey
	mappingKey := strings.Join([]string{"apps", "v1", "Deployment"}, "|")
	val, ok := provider.mappingCache.Get(mappingKey)
	require.True(t, ok, "expected mapping to be cached after first GetObjectMetadata call")
	// val is a schema.GroupVersionResource
	assert.Equal(t, "deployments", val.Resource)

	// Second call: mapping hit path (cache should contain the mapping), provider should still return metadata
	meta2, err := provider.getObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"test": "test"}, meta2.Labels)
}

func TestGetObjectMetadata_CacheKeyUsesUIDOnly(t *testing.T) {
	metricsStore := metrics.NewMetricsStore("test_")
	defer metrics.DestroyMetricsStore(metricsStore)

	provider, cs, dyn, ref := newMetadataTestEnv(t, 10*time.Second)
	var getCalls int32

	dyn.Fake.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&getCalls, 1)
		return false, nil, nil
	})

	_, err := provider.getObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)

	ref.ResourceVersion = "2"
	_, err = provider.getObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&getCalls), "expected cache hit when only ResourceVersion changed")
}

func TestGetObjectMetadata_TTLExpiryTriggersRefresh(t *testing.T) {
	metricsStore := metrics.NewMetricsStore("test_")
	defer metrics.DestroyMetricsStore(metricsStore)

	provider, cs, dyn, ref := newMetadataTestEnv(t, 20*time.Millisecond)
	var getCalls int32

	dyn.Fake.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&getCalls, 1)
		return false, nil, nil
	})

	_, err := provider.getObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	_, err = provider.getObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)

	assert.Equal(t, int32(2), atomic.LoadInt32(&getCalls), "expected cache refresh after TTL expiry")
}
