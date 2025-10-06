package context

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

func TestContextCreation(t *testing.T) {
	ctx := t.Context()

	bindingsCtx := FromContext(ctx)
	assert.Nil(t, bindingsCtx)

	cfg := &genericv1.Config{}
	ctx = WithConfiguration(ctx, cfg)

	// Now context exists
	bindingsCtx = FromContext(ctx)
	require.NotNil(t, bindingsCtx)
	assert.Equal(t, cfg, bindingsCtx.Configuration())
}

func TestWithConfiguration(t *testing.T) {
	ctx := t.Context()
	cfg := &genericv1.Config{}

	ctx = WithConfiguration(ctx, cfg)
	bindingsCtx := FromContext(ctx)

	require.NotNil(t, bindingsCtx)
	assert.Equal(t, cfg, bindingsCtx.Configuration())
}

func TestWithPluginManager(t *testing.T) {
	ctx := t.Context()
	pm := manager.NewPluginManager(ctx)

	ctx = WithPluginManager(ctx, pm)
	bindingsCtx := FromContext(ctx)

	require.NotNil(t, bindingsCtx)
	assert.Equal(t, pm, bindingsCtx.PluginManager())
}

func TestMultipleSetters(t *testing.T) {
	ctx := t.Context()

	cfg := &genericv1.Config{}
	pm := manager.NewPluginManager(ctx)

	// Set all components
	ctx = WithConfiguration(ctx, cfg)
	ctx = WithPluginManager(ctx, pm)

	// Retrieve and verify
	bindingsCtx := FromContext(ctx)
	require.NotNil(t, bindingsCtx)
	assert.Equal(t, cfg, bindingsCtx.Configuration())
	assert.Equal(t, pm, bindingsCtx.PluginManager())
}

func TestNilContext(t *testing.T) {
	var nilCtx *Context

	assert.Nil(t, nilCtx.Configuration())
	assert.Nil(t, nilCtx.PluginManager())
	assert.Nil(t, nilCtx.CredentialGraph())
}

func TestRetrieveOrCreateContext(t *testing.T) {
	// Test with nil context
	ctx, bindingsCtx := retrieveOrCreateContext(nil)
	assert.NotNil(t, ctx)
	assert.NotNil(t, bindingsCtx)

	// Test with existing context
	cfg := &genericv1.Config{}
	ctx = WithConfiguration(context.Background(), cfg)
	ctx2, bindingsCtx2 := retrieveOrCreateContext(ctx)

	// Should reuse existing context
	assert.Equal(t, cfg, bindingsCtx2.Configuration())
	assert.Equal(t, ctx, ctx2)
}

func TestThreadSafety(t *testing.T) {
	ctx := t.Context()
	ctx = WithConfiguration(ctx, &genericv1.Config{})

	bindingsCtx := FromContext(ctx)
	require.NotNil(t, bindingsCtx)

	// Simulate concurrent access
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			// Read operations
			_ = bindingsCtx.Configuration()
			_ = bindingsCtx.PluginManager()
			_ = bindingsCtx.CredentialGraph()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
