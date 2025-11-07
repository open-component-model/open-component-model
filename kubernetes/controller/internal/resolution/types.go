package resolution

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
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

// ResolveResult is an alias for workerpool.ResolveResult for backward compatibility.
type ResolveResult = workerpool.ResolveResult
