// Package cache provides functionality for working with Open Container Initiative (OCI) specifications
// and handling local descriptors saved for later use in a cache.
package cache

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// OCIDescriptorCache defines an interface for temporary storage of OCI descriptors.
// It provides methods to add, retrieve, and delete descs associated with specific references.
// This interface is designed to be used as a temporary storage mechanism before descs are
// added to a component version.
type OCIDescriptorCache interface {
	// Add adds a new oci descriptor to the storage associated with the given reference.
	// The reference is used as a key to group related manifests together.
	Add(reference string, layer ociImageSpecV1.Descriptor)

	// Get retrieves all oci descriptors associated with the given reference.
	// Returns an empty slice if no descriptors are found for the reference.
	Get(reference string) []ociImageSpecV1.Descriptor

	// Delete removes all oci descriptors associated with the given reference.
	// If the reference doesn't exist, this operation is a no-op.
	Delete(reference string)
}
