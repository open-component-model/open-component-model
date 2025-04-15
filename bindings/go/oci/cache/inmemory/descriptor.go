package inmemory

import (
	"sync"

	"github.com/opencontainers/image-spec/specs-go/v1"
)

// InMemory implements the OCIDescriptor interface using an in-memory map.
// It provides thread-safe operations for managing OCI descs in memory.
// This implementation is suitable for temporary storage during component version creation
// but should not be used for long-term persistence.
type InMemory struct {
	mu    sync.RWMutex
	descs map[string][]v1.Descriptor
}

// New creates a new InMemory instance with an initialized
// map for storing descs. This is the recommended way to create a new instance.
func New() *InMemory {
	return &InMemory{
		descs: make(map[string][]v1.Descriptor),
	}
}

func (m *InMemory) Add(reference string, layer v1.Descriptor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.descs[reference] = append(m.descs[reference], layer)
}

func (m *InMemory) Get(reference string) []v1.Descriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.descs[reference]
}

func (m *InMemory) Delete(reference string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.descs, reference)
}
