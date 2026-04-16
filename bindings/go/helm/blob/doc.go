// Package blob provides structured access to Helm chart archives produced by the
// Helm [resource.ResourceRepository].
//
// A [ChartBlob] wraps a tar archive containing a chart .tgz and an optional
// provenance .prov file, exposing each as an individual [blob.ReadOnlyBlob].
// Extraction is lazy and cached in memory once accessed, so repeated calls are cheap.
//
// # Usage
//
//	cb := blob.NewChartBlob(tarBlob)
//
//	chart, err := cb.ChartArchive()  // the .tgz
//	prov, err := cb.ProvFile()       // the .prov, or nil if absent
//
//	// The ChartBlob itself is also a ReadOnlyBlob returning the raw tar.
//	rc, err := cb.ReadCloser()
package blob
