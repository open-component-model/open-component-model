package setup

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/prometheus/client_golang/prometheus"
	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	ocicache "ocm.software/open-component-model/bindings/go/oci/cache"
)

const (
	// BlobCacheKindMetadata is the kind label of manifests, configs and descriptors,
	// the blobs [ocicache.DefaultAccept] admits.
	BlobCacheKindMetadata = "metadata"
	// BlobCacheKindBlob is the kind label of every other blob, such as layers.
	BlobCacheKindBlob = "blob"
)

// BlobCacheFetches counts the fetches of the OCI blob cache by kind and by
// how the cache served them ([ocicache.FetchResult]).
var BlobCacheFetches = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "ocm_system",
	Subsystem: "ocm_k8s_toolkit",
	Name:      "oci_blob_cache_fetches_total",
	Help:      "Fetches of the OCI blob cache by kind (metadata, blob) and result (hit, miss, bypass).",
}, []string{"kind", "result"})

func init() {
	kmetrics.Registry.MustRegister(BlobCacheFetches)
}

func countBlobCacheFetch(desc ociImageSpecV1.Descriptor, result ocicache.FetchResult) {
	kind := BlobCacheKindBlob
	if ocicache.DefaultAccept(desc) {
		kind = BlobCacheKindMetadata
	}
	BlobCacheFetches.WithLabelValues(kind, string(result)).Inc()
}
