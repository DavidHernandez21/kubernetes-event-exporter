package kube

import (
	"context"
	"strings"
	"time"

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

type objectMetadataProvider interface {
	getObjectMetadata(reference *v1.ObjectReference, clientset kubernetes.Interface, dynClient dynamic.Interface, metricsStore *metrics.Store) (objectMetadata, error)
}

type objectMetadataCache struct {
	cache        *lru.TwoQueueCache[string, cachedMetadata]
	mappingCache *lru.TwoQueueCache[string, schema.GroupVersionResource]
	ttl          time.Duration
}

var _ objectMetadataProvider = &objectMetadataCache{}

type cachedMetadata struct {
	fetchedAt time.Time
	metadata  objectMetadata
}

type objectMetadata struct {
	Annotations     map[string]string
	Labels          map[string]string
	OwnerReferences []metav1.OwnerReference
	Deleted         bool
}

func newObjectMetadataProviderWithTTL(size, mappingCacheSize int, ttl time.Duration) objectMetadataProvider {
	if ttl <= 0 {
		panic("cannot init cache: CacheTTL must be positive")
	}

	cache, err := lru.New2Q[string, cachedMetadata](size)
	if err != nil {
		panic("cannot init cache: " + err.Error())
	}

	mappingCache, err := lru.New2Q[string, schema.GroupVersionResource](mappingCacheSize)
	if err != nil {
		panic("cannot init mapping cache: " + err.Error())
	}

	var o objectMetadataProvider = &objectMetadataCache{
		cache:        cache,
		mappingCache: mappingCache,
		ttl:          ttl,
	}

	return o
}

func (o *objectMetadataCache) getObjectMetadata(reference *v1.ObjectReference, clientset kubernetes.Interface, dynClient dynamic.Interface, metricsStore *metrics.Store) (objectMetadata, error) {
	cacheKey := string(reference.UID)
	if val, ok := o.cache.Get(cacheKey); ok {
		if time.Since(val.fetchedAt) < o.ttl || o.ttl <= 0 {
			metricsStore.KubeApiReadCacheHits.Inc()
			return val.metadata, nil
		}
		o.cache.Remove(cacheKey)
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

	mappingKey := group + "|" + version + "|" + reference.Kind

	var gvr schema.GroupVersionResource
	if val, ok := o.mappingCache.Get(mappingKey); ok {
		metricsStore.KubeApiMappingCacheHits.Inc()
		log.Debug().Str("mappingKey", mappingKey).Msg("mapping cache hit")
		gvr = val
	} else {

		groupResources, err := restmapper.GetAPIGroupResources(clientset.Discovery())
		if err != nil {
			return objectMetadata{}, err
		}
		rm := restmapper.NewDiscoveryRESTMapper(groupResources)
		gk := schema.GroupKind{Group: group, Kind: reference.Kind}
		mapping, err := rm.RESTMapping(gk, version)
		if err != nil {
			return objectMetadata{}, err
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
		return objectMetadata{}, err
	}

	om := objectMetadata{
		OwnerReferences: item.GetOwnerReferences(),
		Labels:          item.GetLabels(),
		Annotations:     item.GetAnnotations(),
	}

	if item.GetDeletionTimestamp() != nil {
		om.Deleted = true
	}

	o.cache.Add(cacheKey, cachedMetadata{metadata: om, fetchedAt: time.Now()})
	return om, nil
}
