package kube

import (
	"context"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/resmoio/kubernetes-event-exporter/pkg/metrics"
	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
)

type ObjectMetadataProvider interface {
	GetObjectMetadata(reference *v1.ObjectReference, clientset kubernetes.Interface, dynClient dynamic.Interface, metricsStore *metrics.Store) (ObjectMetadata, error)
}

type ObjectMetadataCache struct {
	cache        *lru.TwoQueueCache[string, ObjectMetadata]
	mappingCache *lru.TwoQueueCache[string, schema.GroupVersionResource]
}

var _ ObjectMetadataProvider = &ObjectMetadataCache{}

type ObjectMetadata struct {
	Annotations     map[string]string
	Labels          map[string]string
	OwnerReferences []metav1.OwnerReference
	Deleted         bool
}

func NewObjectMetadataProvider(size int, mappingCacheSize int) ObjectMetadataProvider {
	cache, err := lru.New2Q[string, ObjectMetadata](size)
	if err != nil {
		panic("cannot init cache: " + err.Error())
	}

	mappingCache, err := lru.New2Q[string, schema.GroupVersionResource](mappingCacheSize)
	if err != nil {
		panic("cannot init mapping cache: " + err.Error())
	}

	var o ObjectMetadataProvider = &ObjectMetadataCache{
		cache:        cache,
		mappingCache: mappingCache,
	}

	return o
}

func (o *ObjectMetadataCache) GetObjectMetadata(reference *v1.ObjectReference, clientset kubernetes.Interface, dynClient dynamic.Interface, metricsStore *metrics.Store) (ObjectMetadata, error) {
	// ResourceVersion changes when the object is updated.
	// We use "UID/ResourceVersion" as cache key so that if the object is updated we get the new metadata.
	cacheKey := strings.Join([]string{string(reference.UID), reference.ResourceVersion}, "/")
	if val, ok := o.cache.Get(cacheKey); ok {
		metricsStore.KubeApiReadCacheHits.Inc()
		return val, nil
	}

	var group, version string
	s := strings.Split(reference.APIVersion, "/")
	if len(s) == 1 {
		group = ""
		version = s[0]
	} else {
		group = s[0]
		version = s[1]
	}

	mappingKey := strings.Join([]string{group, version, reference.Kind}, "|")

	var gvr schema.GroupVersionResource
	if val, ok := o.mappingCache.Get(mappingKey); ok {
		metricsStore.KubeApiMappingCacheHits.Inc()
		log.Debug().Str("mappingKey", mappingKey).Msg("mapping cache hit")
		gvr = val
	} else {

		groupResources, err := restmapper.GetAPIGroupResources(clientset.Discovery())
		if err != nil {
			return ObjectMetadata{}, err
		}
		rm := restmapper.NewDiscoveryRESTMapper(groupResources)
		gk := schema.GroupKind{Group: group, Kind: reference.Kind}
		mapping, err := rm.RESTMapping(gk, version)
		if err != nil {
			return ObjectMetadata{}, err
		}

		metricsStore.KubeApiMappingReadRequests.Inc()
		gvr = mapping.Resource

		o.mappingCache.Add(mappingKey, gvr)
	}

	item, err := dynClient.
		Resource(gvr).
		Namespace(reference.Namespace).
		Get(context.Background(), reference.Name, metav1.GetOptions{})

	metricsStore.KubeApiReadRequests.Inc()

	if err != nil {
		return ObjectMetadata{}, err
	}

	objectMetadata := ObjectMetadata{
		OwnerReferences: item.GetOwnerReferences(),
		Labels:          item.GetLabels(),
		Annotations:     item.GetAnnotations(),
	}

	if item.GetDeletionTimestamp() != nil {
		objectMetadata.Deleted = true
	}

	o.cache.Add(cacheKey, objectMetadata)
	return objectMetadata, nil
}
