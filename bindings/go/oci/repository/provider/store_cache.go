package provider

import (
	"context"
	"sync"

	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
)

type storeCache struct {
	mu    sync.Mutex
	store map[string]*ocictf.Store
}

// loadOrStore returns the existing oci store for the path, if present.
// Otherwise, it uses the load function to get a new oci store, and caches and
// returns this new oci store.
func (c *storeCache) loadOrStore(_ context.Context, path string, load func(path string) (*ocictf.Store, error)) (*ocictf.Store, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if store, ok := c.store[path]; ok {
		return store, nil
	}
	store, err := load(path)
	if err != nil {
		return nil, err
	}
	c.store[path] = store

	return store, nil
}
