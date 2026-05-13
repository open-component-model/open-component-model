package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialRepositoryPluginContract provides a contract for credential plugins to implement.
// This contract holds ConsumerIdentityForConfig, which will return the identity of the credential plugin. And Resolve,
// which uses the credential graph to resolve any credentials.
type CredentialRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	ConsumerIdentityForConfig(ctx context.Context, cfg ConsumerIdentityForConfigRequest[T]) (runtime.Identity, error)

	// ResolveTyped resolves credentials for a given repository configuration and consumer
	// identity, returning a runtime.Typed credential. credentials may be nil.
	ResolveTyped(ctx context.Context, cfg ResolveRequest[T], credentials runtime.Typed) (runtime.Typed, error)

	// Deprecated: Migrate to ResolveTyped for typed credential support.
	// https://github.com/open-component-model/ocm-project/issues/1047
	Resolve(ctx context.Context, cfg ResolveRequest[T], credentials map[string]string) (map[string]string, error)
}
