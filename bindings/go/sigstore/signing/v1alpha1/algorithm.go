package v1alpha1

const (
	// AlgorithmSigstore is the identifier for the Sigstore signing algorithm.
	// Kept identical to the sigstore handler so bundles are interoperable.
	AlgorithmSigstore = "sigstore"

	// MediaTypeSigstoreBundle is the media type for a Sigstore protobuf bundle encoded as JSON.
	MediaTypeSigstoreBundle = "application/vnd.dev.sigstore.bundle.v0.3+json"
)
