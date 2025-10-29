package resolution

import (
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// ResolveOptions contains all the options the resolution service requires to perform a resolve operation.
// The RepositorySpec, Component, Version, the accumulated configuration, the namespace for the resolved configuration.
type ResolveOptions struct {
	RepositorySpec    runtime.Typed
	Component         string
	Version           string
	OCMConfigurations []v1alpha1.OCMConfiguration
	Namespace         string
}

// ResolveResult contains the descriptor, repository and the compute hash of the config for further processing.
type ResolveResult struct {
	Descriptor *descriptor.Descriptor
	Repository repository.ComponentVersionRepository
	ConfigHash []byte
}
