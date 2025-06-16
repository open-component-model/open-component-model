package constructor

import (
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Options are the options for construction based on a *constructor.Constructor.
type Options struct {
	// While constructing a component version, the constructor library will use the given target repository provider
	// to get the target repository for the component specification.
	// The TargetRepositoryProvider is MANDATORY.
	TargetRepositoryProvider

	// While constructing a component version, the constructor library will use the given resource repository provider
	// to get the resource repository for the component specification when processing resources by value.
	// The ResourceRepositoryProvider is OPTIONAL, if no resources need to be processed by value.
	ResourceRepositoryProvider

	// While constructing a component version, the constructor library will use the given resource input method provider
	// to get the resource input method for the component specification when processing resources with an input method.
	// The ResourceInputMethodProvider is OPTIONAL if no resources need to be processed.
	ResourceInputMethodProvider

	// While constructing a component version, the constructor library will use the given source input method provider
	// to get the source input method for the component specification when processing sources with an input method.
	// The SourceInputMethodProvider is OPTIONAL if no sources need to be processed.
	SourceInputMethodProvider

	// While constructing a component version, the constructor library will use the given digest processor provider
	// to get the digest processor for the component specification when processing resources by reference to amend
	// digest information.
	// The ResourceDigestProcessorProvider is OPTIONAL, if not provided, the constructor library will not resolve digests.
	ResourceDigestProcessorProvider

	// While constructing a component version, the constructor library will use the given credential provider
	// to resolve credentials for the input methods.
	// The CredentialProvider is OPTIONAL, if not provided, the constructor library will not resolve credentials.
	CredentialProvider

	// While constructing a component version, the constructor library will use the given function
	// to determine if a resource should be processed by value.
	// The ProcessResourceByValue is OPTIONAL, if not provided, the constructor library will not process resources by value.
	ProcessResourceByValue func(resource *constructor.Resource) bool

	// While constructing a component version, the constructor library will use the given concurrency limit
	// to limit the number of concurrent operations.
	// The ConcurrencyLimit is OPTIONAL, if not provided, the constructor library will use the number of CPU cores.
	ConcurrencyLimit int

	// While constructing a component version, the constructor library will use the policy to determine how to handle conflicts
	// of component versions when interacting with the target repository.
	ComponentVersionConflictPolicy

	OnFinishedConstructed        func(descriptor *runtime.Descriptor) error
	OnStartedConstructing        func(component *constructor.Component) error
	OnStartedProcessingResource  func(component *constructor.Component, resource *constructor.Resource) error
	OnStartedProcessingSource    func(component *constructor.Component, source *constructor.Source) error
	OnFinishedProcessingResource func(component *constructor.Component, resource *runtime.Resource) error
	OnFinishedProcessingSource   func(component *constructor.Component, source *runtime.Source) error
}

// ComponentVersionConflictPolicy defines the policy for handling component version conflicts
// when interacting with the target repository.
// If the constructor library encounters a component version that already exists in the target repository.
type ComponentVersionConflictPolicy int

const (
	// ComponentVersionConflictAbortAndFail will abort the construction process if a component version already exists in the target repository.
	// This is the default policy.
	ComponentVersionConflictAbortAndFail ComponentVersionConflictPolicy = iota
	// ComponentVersionConflictReplace will replace the existing component version in the target repository with the new one.
	// This will overwrite the existing component version.
	ComponentVersionConflictReplace
	// ComponentVersionConflictSkip will skip the construction of the component version if it already exists in the target repository.
	ComponentVersionConflictSkip
)
