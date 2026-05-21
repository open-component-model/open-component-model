package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialRepositoryPluginContract provides a contract for credential plugins to implement.
// It exposes ConsumerIdentityForConfig, which returns the consumer identity for a repository
// configuration, and Resolve, which uses the credential graph to resolve credentials.
type CredentialRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	ConsumerIdentityForConfig(ctx context.Context, cfg ConsumerIdentityForConfigRequest[T]) (runtime.Identity, error)

	// Resolve resolves credentials for a given repository configuration and consumer
	// identity, returning a runtime.Typed credential. credentials may be nil.
	Resolve(ctx context.Context, cfg ResolveRequest[T], credentials runtime.Typed) (runtime.Typed, error)
}
