// Package context provides a context wrapper for the new OCM bindings system.
package context

import (
	"context"
	"sync"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

type ctxKey string

const key ctxKey = "ocm.software/open-component-model/kubernetes/controller/internal/bindings"

// Context holds the OCM bindings components for the controller.
// It provides centralized access to configuration, plugin manager, and credential resolution.
// This context is thread-safe and can be safely accessed from multiple goroutines.
type Context struct {
	mu              sync.RWMutex
	configuration   *genericv1.Config
	pluginManager   *manager.PluginManager
	credentialGraph credentials.Resolver
}

// WithConfiguration creates a new context with the given configuration.
func WithConfiguration(ctx context.Context, cfg *genericv1.Config) context.Context {
	ctx, octx := retrieveOrCreateContext(ctx)
	octx.mu.Lock()
	defer octx.mu.Unlock()
	octx.configuration = cfg
	return ctx
}

// WithPluginManager creates a new context with the given plugin manager.
func WithPluginManager(ctx context.Context, pm *manager.PluginManager) context.Context {
	ctx, octx := retrieveOrCreateContext(ctx)
	octx.mu.Lock()
	defer octx.mu.Unlock()
	octx.pluginManager = pm
	return ctx
}

// WithCredentialGraph creates a new context with the given credential graph.
func WithCredentialGraph(ctx context.Context, graph credentials.Resolver) context.Context {
	ctx, octx := retrieveOrCreateContext(ctx)
	octx.mu.Lock()
	defer octx.mu.Unlock()
	octx.credentialGraph = graph
	return ctx
}

// Configuration returns the OCM configuration from the context.
// Returns nil if no configuration has been set.
func (c *Context) Configuration() *genericv1.Config {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configuration
}

// PluginManager returns the plugin manager from the context.
// Returns nil if no plugin manager has been set.
func (c *Context) PluginManager() *manager.PluginManager {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pluginManager
}

// CredentialGraph returns the credential graph from the context.
// Returns nil if no credential graph has been set.
func (c *Context) CredentialGraph() credentials.Resolver {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.credentialGraph
}

// FromContext retrieves the bindings context from the given context.
// Returns nil if no bindings context exists.
func FromContext(ctx context.Context) *Context {
	if ctx == nil {
		return nil
	}

	if v, ok := ctx.Value(key).(*Context); ok {
		return v
	}
	return nil
}

// WithContext creates a new context with the given bindings context.
func WithContext(ctx context.Context, c *Context) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, key, c)
}

// retrieveOrCreateContext retrieves the bindings context from the given context.
// If no context exists, it creates a new one and returns it.
func retrieveOrCreateContext(ctx context.Context) (context.Context, *Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	octx := FromContext(ctx)
	if octx == nil {
		octx = &Context{}
		ctx = WithContext(ctx, octx)
	}
	return ctx, octx
}
