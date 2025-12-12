package constructor

import (
	"context"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Options are the options for construction based on a *constructor.Constructor.
type Options struct {
	// While constructing a component version, the constructor library will use the given target repository provider
	// to get the target repository for the component specification.
	// The TargetRepositoryProvider is MANDATORY.
	TargetRepositoryProvider

	// While constructing a component version, the constructor library will use the given external component version
	// repository provider to get the repository to resolve external referenced components. So, components that are not
	// part of the construction specification.
	// The ExternalComponentRepositoryProvider is OPTIONAL, if no externally referenced components need to be resolved.
	ExternalComponentRepositoryProvider

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
	// The Resolver is OPTIONAL, if not provided, the constructor library will not resolve credentials.
	credentials.Resolver

	// While constructing a component version, the constructor library will use the given concurrency limit
	// to limit the number of concurrent operations.
	// The ConcurrencyLimit is OPTIONAL, if not provided, the constructor library will use the number of CPU cores.
	ConcurrencyLimit int

	// While constructing a component version, the constructor library will use the policy to determine how to handle conflicts
	// of component versions when interacting with the target repository.
	ComponentVersionConflictPolicy

	// While constructing a component version, the constructor library will use the policy to determine how to handle
	// external references to component versions not located within the constructor or target repository itself.
	ExternalComponentVersionCopyPolicy

	// While constructing a component version, the constructor library will use the given callbacks to notify about
	// the construction process. This can be used to implement custom logging or other actions such as progress trackers.
	ComponentConstructionCallbacks
}

type ComponentConstructionCallbacks struct {
	// OnStartComponentConstruct is called before the construction of a component version starts.
	OnStartComponentConstruct func(ctx context.Context, component *constructor.Component) error
	// OnEndComponentConstruct is called after the construction of a component version ends.
	// If an error occurs during the construction, the error is passed as a parameter.
	OnEndComponentConstruct func(ctx context.Context, descriptor *descriptor.Descriptor, err error) error

	// OnStartResourceConstruct is called before the construction of a resource starts.
	OnStartResourceConstruct func(ctx context.Context, resource *constructor.Resource) error
	// OnEndResourceConstruct is called after the construction of a resource ends.
	// If an error occurs during the construction, the error is passed as a parameter.
	OnEndResourceConstruct func(ctx context.Context, resource *descriptor.Resource, err error) error

	// OnStartSourceConstruct is called before the construction of a source starts.
	OnStartSourceConstruct func(ctx context.Context, source *constructor.Source) error
	// OnEndSourceConstruct is called after the construction of a source ends.
	// If an error occurs during the construction, the error is passed as a parameter.
	OnEndSourceConstruct func(ctx context.Context, source *descriptor.Source, err error) error

	// OnStartReferenceConstruct is called before the construction of a component reference starts.
	OnStartReferenceConstruct func(ctx context.Context, reference *constructor.Reference) error
	// OnEndReferenceConstruct is called after the construction of a component reference ends.
	// If an error occurs during the construction, the error is passed as a parameter.
	OnEndReferenceConstruct func(ctx context.Context, reference *descriptor.Reference, err error) error
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

// ExternalComponentVersionCopyPolicy defines the policy for handling external component version references
// when interacting with the target repository.
// If the constructor library encounters an external component version reference that is not part of the constructor
// specification, it will use this policy to determine how to handle the reference.
type ExternalComponentVersionCopyPolicy int

const (
	// ExternalComponentVersionCopyPolicySkip will skip the copy of the component version to the target repository.
	// This is the default policy.
	ExternalComponentVersionCopyPolicySkip = iota
	// ExternalComponentVersionCopyPolicyCopyOrFail will copy the external component version to the target repository.
	// If a copy fails, the construction process will fail.
	ExternalComponentVersionCopyPolicyCopyOrFail ExternalComponentVersionCopyPolicy = iota
)
