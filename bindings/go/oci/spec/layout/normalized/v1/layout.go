// Package v1 defines constants and helpers for the normalized (cosign-signable) OCM OCI layout.
//
// In this layout the tag resolves to a normalized, access-free descriptor manifest whose digest
// is stable across registry-to-registry copies (it contains no access/location information). The
// access-bearing descriptor and local blobs are stored as a referrer of that manifest and are
// regenerated per registry.
package v1

// LayoutVersion is the version of the normalized layout implemented by this package.
const LayoutVersion = "v1"

const (
	// AnnotationLayoutVersion records the layout version on the normalized manifest.
	AnnotationLayoutVersion = "software.ocm.component-model/layout-version"
	// AnnotationNormalisationAlgo records the normalisation algorithm used to produce the
	// normalized descriptor layer, so the bind check is unambiguous and reproducible.
	AnnotationNormalisationAlgo = "software.ocm.component-model/normalisation-algo"
)
