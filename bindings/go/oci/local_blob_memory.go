// Package oci provides functionality for working with Open Container Initiative (OCI) specifications
// and handling local blob storage in memory.
package oci

import (
	"sync"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// LocalBlobMemory defines an interface for temporary storage of OCI blobs.
// It provides methods to add, retrieve, and delete blobs associated with specific references.
// This interface is designed to be used as a temporary storage mechanism before blobs are
// added to a component version.
type LocalBlobMemory interface {
	// AddBlob adds a new blob layer to the storage associated with the given reference.
	// The reference is used as a key to group related blobs together.
	AddBlob(reference string, layer ociImageSpecV1.Descriptor)

	// GetBlobs retrieves all blob layers associated with the given reference.
	// Returns an empty slice if no blobs are found for the reference.
	GetBlobs(reference string) []ociImageSpecV1.Descriptor

	// DeleteBlobs removes all blob layers associated with the given reference.
	// If the reference doesn't exist, this operation is a no-op.
	DeleteBlobs(reference string)
}

// InMemoryLocalBlobMemory implements the LocalBlobMemory interface using an in-memory map.
// It provides thread-safe operations for managing OCI blobs in memory.
// This implementation is suitable for temporary storage during component version creation
// but should not be used for long-term persistence.
type InMemoryLocalBlobMemory struct {
	mu    sync.RWMutex
	blobs map[string][]ociImageSpecV1.Descriptor
}

// NewLocalBlobMemory creates a new InMemoryLocalBlobMemory instance with an initialized
// map for storing blobs. This is the recommended way to create a new instance.
func NewLocalBlobMemory() *InMemoryLocalBlobMemory {
	return &InMemoryLocalBlobMemory{
		blobs: make(map[string][]ociImageSpecV1.Descriptor),
	}
}

// AddBlob adds a blob layer to the memory store associated with the given reference.
// The operation is thread-safe and will append the layer to any existing blobs
// for the same reference.
func (m *InMemoryLocalBlobMemory) AddBlob(reference string, layer ociImageSpecV1.Descriptor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[reference] = append(m.blobs[reference], layer)
}

// GetBlobs retrieves all blob layers associated with the given reference.
// The operation is thread-safe and returns a copy of the stored blobs.
// Returns an empty slice if no blobs are found for the reference.
func (m *InMemoryLocalBlobMemory) GetBlobs(reference string) []ociImageSpecV1.Descriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blobs[reference]
}

// DeleteBlobs removes all blob layers associated with the given reference.
// The operation is thread-safe and will remove the reference entry from the map.
// If the reference doesn't exist, this operation is a no-op.
func (m *InMemoryLocalBlobMemory) DeleteBlobs(reference string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, reference)
}
