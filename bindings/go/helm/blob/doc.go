// Package blob provides structured access to Helm chart archives produced by the
// Helm ResourceRepository. A [ChartBlob] wraps a tar archive and exposes the
// chart (.tgz) and optional provenance (.prov) files as individual blobs.
package blob
