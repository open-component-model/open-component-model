// Package resource implements a [repository.ResourceRepository] for Helm charts
// hosted in classic Helm repositories. It downloads charts (and optional
// provenance files) from remote HTTP/HTTPS or OCI-based Helm repos and returns
// them as tar-archived blobs via [blob.ChartBlob].
package resource
