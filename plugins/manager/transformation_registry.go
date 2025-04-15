package manager

import (
	"context"
	"errors"
	"os"
	"sync"
)

type TransformationRegistry struct {
	registry           map[string]map[string]map[string]any
	constructedPlugins map[string]*constructedPlugin
	mu                 sync.Mutex
}

func NewTransformationRegistry() *TransformationRegistry {
	return &TransformationRegistry{
		registry:           make(map[string]map[string]map[string]any),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}

func (r *TransformationRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs error
	for _, p := range r.constructedPlugins {
		// The plugins should handle the Interrupt signal for shutdowns.
		// TODO: Use context to wait for the plugin to actually shut down.
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil {
			errs = errors.Join(errs, perr)
		}
	}

	return errs
}
