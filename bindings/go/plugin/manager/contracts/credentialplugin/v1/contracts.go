package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialPluginContract provides a contract for credential plugins to implement.
// GetConsumerIdentity returns the consumer identity for a given credential specification.
// Resolve uses the credential graph to resolve credentials for a given identity.
type CredentialPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	GetConsumerIdentity(ctx context.Context, request GetConsumerIdentityRequest[T]) (runtime.Identity, error)
	Resolve(ctx context.Context, request ResolveRequest[T], credentials runtime.Typed) (runtime.Typed, error)
}
