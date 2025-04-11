// Package memory provides functionality for working with Open Container Initiative (OCI) specifications
// and handling local descriptors saved for later use in memory.
package memory

import (
	"sync"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// LocalDescriptorMemory defines an interface for temporary storage of OCI descriptors.
// It provides methods to add, retrieve, and delete descs associated with specific references.
// This interface is designed to be used as a temporary storage mechanism before descs are
// added to a component version.
type LocalDescriptorMemory interface {
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

// InMemory implements the LocalDescriptorMemory interface using an in-memory map.
// It provides thread-safe operations for managing OCI descs in memory.
// This implementation is suitable for temporary storage during component version creation
// but should not be used for long-term persistence.
type InMemory struct {
	mu    sync.RWMutex
	descs map[string][]ociImageSpecV1.Descriptor
}

// NewInMemory creates a new InMemory instance with an initialized
// map for storing descs. This is the recommended way to create a new instance.
func NewInMemory() *InMemory {
	return &InMemory{
		descs: make(map[string][]ociImageSpecV1.Descriptor),
	}
}

func (m *InMemory) Add(reference string, layer ociImageSpecV1.Descriptor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.descs[reference] = append(m.descs[reference], layer)
}

func (m *InMemory) Get(reference string) []ociImageSpecV1.Descriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.descs[reference]
}

func (m *InMemory) Delete(reference string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.descs, reference)
}
