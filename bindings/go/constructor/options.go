package constructor

import (
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
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
}
