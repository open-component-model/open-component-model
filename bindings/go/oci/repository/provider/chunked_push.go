package provider

import (
	"strings"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ociRepositoryHost derives the URL host (hostname[:port], as in net/url.URL.Host)
// from the repository BaseUrl, the same way NewResolver extracts it, so the
// chunked-push host match lines up with the HTTP client's per-host matching.
// Returns an empty string when the BaseUrl cannot be parsed; an empty host
// matches no per-host entry and therefore resolves to the global config.
func ociRepositoryHost(obj *ocirepospecv1.Repository) string {
	purl, err := runtime.ParseURLAndAllowNoScheme(strings.TrimSuffix(obj.BaseUrl, "/"))
	if err != nil {
		return ""
	}
	return purl.Host
}

// chunkedPushResolverOption returns a resolver option that overrides the
// NewResolver chunked-push default for host, or ok=false when no override is
// configured (leaving the default-on behaviour).
//
// Precedence mirrors the HTTP client's host matching (see Config.ResolveHost):
// a per-host ChunkedPush overrides the global one wholesale; a host with no
// entry falls back to the global ChunkedPush. Disabled maps to chunk size 0,
// which disables chunking (monolithic push). Otherwise a 0 size or threshold
// maps to the remotestore default so the config package stays free of any
// oci/* dependency.
func chunkedPushResolverOption(cfg *httpv1alpha1.Config, host string) (urlresolver.Option, bool) {
	if cfg == nil {
		return nil, false
	}
	c := cfg.ResolveHost(host).ChunkedPush
	if c == nil {
		return nil, false
	}
	if c.Disabled {
		return urlresolver.WithChunkedPush(0, 0), true
	}
	size := c.ChunkSize
	if size == 0 {
		size = remotestore.DefaultChunkSize
	}
	threshold := c.Threshold
	if threshold == 0 {
		threshold = remotestore.DefaultChunkThreshold
	}
	return urlresolver.WithChunkedPush(size, threshold), true
}
