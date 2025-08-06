package context

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

func TestWithFilesystemConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   *filesystemv1alpha1.Config
		expected *filesystemv1alpha1.Config
	}{
		{
			name: "basic filesystem config",
			config: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expected: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
		},
		{
			name:     "empty filesystem config",
			config:   &filesystemv1alpha1.Config{},
			expected: &filesystemv1alpha1.Config{},
		},
		{
			name:     "nil filesystem config",
			config:   nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create context with filesystem config
			ctx := WithFilesystemConfig(context.Background(), tt.config)

			// Retrieve and verify
			ocmCtx := FromContext(ctx)
			r.NotNil(ocmCtx, "OCM context should be available")

			result := ocmCtx.FilesystemConfig()
			if tt.expected == nil {
				r.Nil(result, "filesystem config should be nil")
			} else {
				r.NotNil(result, "filesystem config should not be nil")
				r.Equal(tt.expected.TempFolder, result.TempFolder, "temp folder should match")
			}
		})
	}
}

func TestFilesystemConfigFromNilContext(t *testing.T) {
	r := require.New(t)

	// Test retrieving from nil context
	var nilCtx *Context
	result := nilCtx.FilesystemConfig()
	r.Nil(result, "filesystem config should be nil from nil context")
}

func TestFilesystemConfigConcurrentAccess(t *testing.T) {
	r := require.New(t)

	// Create context with filesystem config
	initialConfig := &filesystemv1alpha1.Config{
		TempFolder: "/tmp/initial",
	}
	ctx := WithFilesystemConfig(context.Background(), initialConfig)

	// Test concurrent reads
	done := make(chan bool, 10)
	for range 10 {
		go func() {
			defer func() { done <- true }()
			ocmCtx := FromContext(ctx)
			fsCfg := ocmCtx.FilesystemConfig()
			r.NotNil(fsCfg, "filesystem config should be available")
			r.Equal("/tmp/initial", fsCfg.TempFolder, "temp folder should be consistent")
		}()
	}

	// Wait for all goroutines to complete
	for range 10 {
		<-done
	}
}

func TestContextWithMultipleConfigurations(t *testing.T) {
	r := require.New(t)

	// Create a context with multiple configurations
	ctx := context.Background()

	// Add central configuration
	centralConfig := &genericv1.Config{}
	ctx = WithConfiguration(ctx, centralConfig)

	// Add credential graph
	credGraph := &credentials.Graph{}
	ctx = WithCredentialGraph(ctx, credGraph)

	// Add plugin manager
	pluginMgr := manager.NewPluginManager(ctx)
	ctx = WithPluginManager(ctx, pluginMgr)

	// Add filesystem config
	fsConfig := &filesystemv1alpha1.Config{
		TempFolder: "/tmp/multi",
	}
	ctx = WithFilesystemConfig(ctx, fsConfig)

	// Verify all configurations are available
	ocmCtx := FromContext(ctx)
	r.NotNil(ocmCtx, "OCM context should be available")

	r.Equal(centralConfig, ocmCtx.Configuration(), "central config should be available")
	r.Equal(credGraph, ocmCtx.CredentialGraph(), "credential graph should be available")
	r.Equal(pluginMgr, ocmCtx.PluginManager(), "plugin manager should be available")

	retrievedFsConfig := ocmCtx.FilesystemConfig()
	r.NotNil(retrievedFsConfig, "filesystem config should be available")
	r.Equal("/tmp/multi", retrievedFsConfig.TempFolder, "filesystem config should be correct")
}

func TestContextOverwriteFilesystemConfig(t *testing.T) {
	r := require.New(t)

	// Create initial context with filesystem config
	initialConfig := &filesystemv1alpha1.Config{
		TempFolder: "/tmp/initial",
	}
	ctx := WithFilesystemConfig(context.Background(), initialConfig)

	// Verify initial config
	ocmCtx := FromContext(ctx)
	fsCfg := ocmCtx.FilesystemConfig()
	r.Equal("/tmp/initial", fsCfg.TempFolder, "initial config should be set")

	// Overwrite with new config
	newConfig := &filesystemv1alpha1.Config{
		TempFolder: "/tmp/overwrite",
	}
	ctx = WithFilesystemConfig(ctx, newConfig)

	// Verify overwrite
	ocmCtx = FromContext(ctx)
	fsCfg = ocmCtx.FilesystemConfig()
	r.Equal("/tmp/overwrite", fsCfg.TempFolder, "config should be overwritten")
}

func TestContextRetrieveOrCreateOCMContext(t *testing.T) {
	r := require.New(t)

	// Test creating new context
	ctx := context.Background()
	newCtx, ocmCtx := retrieveOrCreateOCMContext(ctx)
	r.NotNil(newCtx, "new context should be created")
	r.NotNil(ocmCtx, "OCM context should be created")

	// Test retrieving existing context
	existingCtx, existingOcmCtx := retrieveOrCreateOCMContext(newCtx)
	r.Equal(newCtx, existingCtx, "should return same context")
	r.Equal(ocmCtx, existingOcmCtx, "should return same OCM context")
}

func TestFromContextWithoutOCMContext(t *testing.T) {
	r := require.New(t)

	// Test retrieving from context without OCM context
	ctx := context.Background()
	ocmCtx := FromContext(ctx)
	r.Nil(ocmCtx, "should return nil when OCM context doesn't exist")
}

func TestFilesystemConfigIsolation(t *testing.T) {
	r := require.New(t)

	// Create two separate contexts with different filesystem configs
	config1 := &filesystemv1alpha1.Config{TempFolder: "/tmp/ctx1"}
	config2 := &filesystemv1alpha1.Config{TempFolder: "/tmp/ctx2"}

	ctx1 := WithFilesystemConfig(context.Background(), config1)
	ctx2 := WithFilesystemConfig(context.Background(), config2)

	// Verify they are isolated
	ocmCtx1 := FromContext(ctx1)
	ocmCtx2 := FromContext(ctx2)

	fsCfg1 := ocmCtx1.FilesystemConfig()
	fsCfg2 := ocmCtx2.FilesystemConfig()

	r.Equal("/tmp/ctx1", fsCfg1.TempFolder, "context 1 should have correct config")
	r.Equal("/tmp/ctx2", fsCfg2.TempFolder, "context 2 should have correct config")
	r.NotEqual(fsCfg1.TempFolder, fsCfg2.TempFolder, "contexts should be isolated")
}
