package resolution

import (
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

type ResolveOptions struct {
	RepositorySpec       runtime.Typed
	Component            string
	Version              string
	OCMConfigurations    []v1alpha1.OCMConfiguration
	Namespace            string
	VerificationProvider v1alpha1.VerificationProvider
}

type ResolveResult struct {
	Descriptor *descriptor.Descriptor
	Repository repository.ComponentVersionRepository
	ConfigHash []byte
}
