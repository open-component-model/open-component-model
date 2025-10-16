package resolution

import (
	"time"

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
	Metadata   ResolveMetadata
	// Error stores any resolution error that occurred during background processing.
	// This allows distinguishing between "in progress" and "failed" states.
	// Nil indicates successful resolution.
	Error error
}

type ResolveMetadata struct {
	ResolvedAt time.Time
	ConfigHash []byte
}
