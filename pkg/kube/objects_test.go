package kube

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/resmoio/kubernetes-event-exporter/pkg/metrics"
)

func TestGetObjectMetadata_MappingCacheMissThenHit_WithFakeClients(t *testing.T) {
	metricsStore := metrics.NewMetricsStore("test_")
	defer metrics.DestroyMetricsStore(metricsStore)

	// provider with empty caches
	provider := NewObjectMetadataProvider(1024, 256).(*ObjectMetadataCache)

	// Prepare fake discovery: declare apps/v1 deployments
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
	// Fake typed clientset (we only need discovery interface)
	cs := fake.NewClientset()
	// underlying discovery is *fakediscovery.FakeDiscovery
	if fd, ok := cs.Discovery().(*fakediscovery.FakeDiscovery); ok {
		fd.Resources = []*metav1.APIResourceList{apiRes}
	} else {
		t.Fatalf("expected fake discovery type")
	}

	// Create an unstructured deployment that dynamic fake will return
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-deploy",
				"namespace": "default",
				"uid":       "test-uid",
				"labels": map[string]interface{}{
					"test": "test",
				},
				"annotations": map[string]interface{}{
					"test": "test",
				},
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "testAPI",
						"kind":       "testKind",
						"name":       "testOwner",
						"uid":        "testOwner",
					},
				},
			},
		},
	}

	// Fake dynamic client seeded with the unstructured object
	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), u)

	// Build an ObjectReference that will trigger mapping resolution
	ref := &corev1.ObjectReference{
		UID:             "test-uid",
		ResourceVersion: "1",
		APIVersion:      "apps/v1",
		Kind:            "Deployment",
		Name:            "test-deploy",
		Namespace:       "default",
	}

	// First call: mapping miss -> provider should call discovery+RESTMapping and then dyn Get
	meta, err := provider.GetObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"test": "test"}, meta.Labels)
	// mapping should have been resolved and cached: assert mappingCache contains the mappingKey
	mappingKey := strings.Join([]string{"apps", "v1", "Deployment"}, "|")
	val, ok := provider.mappingCache.Get(mappingKey)
	require.True(t, ok, "expected mapping to be cached after first GetObjectMetadata call")
	// val is a schema.GroupVersionResource
	assert.Equal(t, "deployments", val.Resource)

	// Second call: mapping hit path (cache should contain the mapping), provider should still return metadata
	meta2, err := provider.GetObjectMetadata(ref, cs, dyn, metricsStore)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"test": "test"}, meta2.Labels)
}
