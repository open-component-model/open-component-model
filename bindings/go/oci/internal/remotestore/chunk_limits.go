package remotestore

import (
	"net"
	"strings"
)

// registryMaxChunkSize reports the largest Content-Range PATCH body a known
// registry accepts. The OCI distribution spec has no header advertising a
// maximum (only OCI-Chunk-Min-Length), so registries that reject the default
// chunk size are listed here. A limit of 0 means the registry does not support
// chunked upload at all. ok is false for any other registry, which then uses
// the configured ChunkSize; a rejected first chunk still falls back to a
// monolithic upload there.
//
// Only documented or measured limits belong here:
//   - ghcr.io rejects a Content-Range PATCH above 4 MiB with 416
//     REQUESTED_RANGE_NOT_SATISFIABLE; exactly 4 MiB is accepted (measured).
//   - Google Artifact Registry ("<location>-docker.pkg.dev") does not support
//     chunked upload: https://cloud.google.com/artifact-registry/docs/reference/docker-api
func registryMaxChunkSize(registry string) (limit int64, ok bool) {
	host := strings.ToLower(registry)
	if h, port, err := net.SplitHostPort(host); err == nil && port == "443" {
		host = h
	}
	switch {
	case host == "ghcr.io":
		return 4 << 20, true
	case strings.HasSuffix(host, "-docker.pkg.dev"):
		return 0, true
	}
	return 0, false
}

// chunkSize returns the PATCH chunk size used for this repository's registry:
// ChunkSize capped by a known registry limit (see registryMaxChunkSize). A
// result <= 0 disables chunked upload.
func (r *RemoteStore) chunkSize() int64 {
	if limit, ok := registryMaxChunkSize(r.Reference.Registry); ok && limit < r.ChunkSize {
		return limit
	}
	return r.ChunkSize
}
