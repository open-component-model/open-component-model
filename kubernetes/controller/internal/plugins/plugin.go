// Package plugins provides initialization functions for OCM components in the controller.
package plugins

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type PluginManagerOptions struct {
	IdleTimeout time.Duration
	Logger      *logr.Logger
	Scheme      *ocmruntime.Scheme
	Provider    repository.ComponentVersionRepositoryProvider
}

// PluginManager contains settings for the plugin manager.
type PluginManager struct {
	IdleTimeout   time.Duration
	Logger        *logr.Logger
	PluginManager *manager.PluginManager
	Scheme        *ocmruntime.Scheme
	Provider      repository.ComponentVersionRepositoryProvider
}

// Start will start the plugin manager with the given configuration. Start will BLOCK until the
// context is cancelled. It's designed to be called by the controller manager.
func (m *PluginManager) Start(ctx context.Context) error {
	pm := manager.NewPluginManager(ctx)
	// TODO(Skarlso): Change the registration process once update lands.
	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(m.Scheme, pm.ComponentVersionRepositoryRegistry, m.Provider, &ociv1.Repository{}); err != nil {
		return fmt.Errorf("failed to register internal component version repository plugin: %w", err)
	}

	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(m.Scheme, pm.ComponentVersionRepositoryRegistry, m.Provider, &ctfv1.Repository{}); err != nil {
		return fmt.Errorf("failed to register internal component version repository plugin: %w", err)
	}

	// Register OCI credential repository plugin to handle credentials for OCI registries
	credScheme := ocmruntime.NewScheme()
	// Register both versioned and unversioned types
	credScheme.MustRegisterWithAlias(
		&ocicredentialsspecv1.DockerConfig{},
		ocicredentialsspec.CredentialRepositoryConfigType,                                     // DockerConfig/v1
		ocmruntime.NewUnversionedType(ocicredentialsspec.CredentialRepositoryConfigType.Name), // DockerConfig
	)
	if err := credentialrepository.RegisterInternalCredentialRepositoryPlugin(
		credScheme,
		pm.CredentialRepositoryRegistry,
		&ocicredentials.OCICredentialRepository{},
		&ocicredentialsspecv1.DockerConfig{},
		[]ocmruntime.Type{credentials.AnyConsumerIdentityType},
	); err != nil {
		return fmt.Errorf("failed to register internal OCI credential repository plugin: %w", err)
	}

	m.PluginManager = pm

	<-ctx.Done() // block until context is done ( expected by the manager )

	// We use context background here because the Start context has been cancelled.
	return pm.Shutdown(context.Background())
}

func (m *PluginManager) Shutdown(ctx context.Context) error {
	if m.PluginManager == nil {
		return nil
	}

	if err := m.PluginManager.Shutdown(ctx); err != nil {
		m.Logger.Error(err, "failed to shutdown plugin manager")
		return fmt.Errorf("plugin manager shutdown failed: %w", err)
	}

	return nil
}

// NewPluginManager creates and initializes a plugin manager with the given configuration.
// It registers plugins from the configured Locations and built-in plugins.
func NewPluginManager(opts PluginManagerOptions) *PluginManager {
	return &PluginManager{
		Logger:      opts.Logger,
		IdleTimeout: opts.IdleTimeout,
		Scheme:      opts.Scheme,
		Provider:    opts.Provider,
	}
}
