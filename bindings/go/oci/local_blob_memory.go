package oci

import (
	"sync"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// LocalBlobMemory is a temporary storage for local blobs until they are added to a component version.
// TODO: Implement a file-based cache similar to OCI Layout's "ingest" directory.
type LocalBlobMemory struct {
	sync.RWMutex
	blobs map[string][]ociImageSpecV1.Descriptor
}

// NewLocalBlobMemory creates a new LocalBlobMemory instance.
func NewLocalBlobMemory() *LocalBlobMemory {
	return &LocalBlobMemory{
		blobs: make(map[string][]ociImageSpecV1.Descriptor),
	}
}

// AddBlob adds a blob to the memory store.
func (m *LocalBlobMemory) AddBlob(reference string, layer ociImageSpecV1.Descriptor) {
	m.Lock()
	defer m.Unlock()
	m.blobs[reference] = append(m.blobs[reference], layer)
}

// GetBlobs retrieves all blobs for a reference.
func (m *LocalBlobMemory) GetBlobs(reference string) []ociImageSpecV1.Descriptor {
	m.RLock()
	defer m.RUnlock()
	return m.blobs[reference]
}

// DeleteBlobs removes all blobs for a reference.
func (m *LocalBlobMemory) DeleteBlobs(reference string) {
	m.Lock()
	defer m.Unlock()
	delete(m.blobs, reference)
}
