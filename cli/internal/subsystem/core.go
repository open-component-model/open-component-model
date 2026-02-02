package subsystem

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// NewRegistryFromPluginManager creates a new subsystem registry populated with
// all core subsystems and registers the plugin manager's schemes with each subsystem.
func NewRegistryFromPluginManager(pm *manager.PluginManager) (*Registry, error) {
	registry := NewRegistry()

	// Create subsystems locally
	ocmRepository := NewSubsystem(
		"ocm-repository",
		"Repositories for storing and managing OCM component versions.",
	)
	ocmRepositoryLister := NewSubsystem(
		"ocm-repository-lister",
		"Listers for listing OCM component repositories. Can be seen as repository of versioned repositories",
	)
	ocmResourceRepository := NewSubsystem(
		"ocm-resource-repository",
		"Repositories for storing and managing OCM resources.",
	)
	input := NewSubsystem(
		"input",
		"Input methods define how content is sourced and ingested into an OCM component version.",
	)
	credentialRepository := NewSubsystem(
		"credential-repository",
		"Repositories for storing and managing credentials so they can be referenced in the OCM credential graph.",
	)
	signingHandler := NewSubsystem(
		"signing",
		"Signing handlers are responsible for signing and verification of component versions.",
	)

	// Register plugin manager schemes
	if err := errors.Join(
		ocmRepository.Scheme.RegisterScheme(pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryScheme()),
		ocmRepositoryLister.Scheme.RegisterScheme(pm.ComponentListerRegistry.GetComponentVersionRepositoryScheme()),
		ocmResourceRepository.Scheme.RegisterScheme(pm.ResourcePluginRegistry.ResourceScheme()),
		input.Scheme.RegisterScheme(pm.InputRegistry.InputRepositoryScheme()),
		credentialRepository.Scheme.RegisterScheme(pm.CredentialRepositoryRegistry.RepositoryScheme()),
		signingHandler.Scheme.RegisterScheme(pm.SigningRegistry.ResourceScheme()),
	); err != nil {
		return nil, err
	}

	// Register all subsystems
	registry.Register(ocmRepository)
	registry.Register(ocmRepositoryLister)
	registry.Register(ocmResourceRepository)
	registry.Register(input)
	registry.Register(credentialRepository)
	registry.Register(signingHandler)

	return registry, nil
}
