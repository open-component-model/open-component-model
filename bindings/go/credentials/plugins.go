package credentials

import (
	"context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

var AnyCredentialType = runtime.NewUnversionedType("*")

type RepositoryPlugin interface {
	SupportedRepositoryConfigTypes() []runtime.Type
	ConsumerIdentityForConfig(ctx context.Context, config runtime.Typed) (runtime.Identity, error)
	Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error)
}

type CredentialPlugin interface {
	GetConsumerIdentity(ctx context.Context, credential runtime.Typed) (runtime.Identity, error)
	Resolve(ctx context.Context, identity runtime.Identity, credentials map[string]string) (map[string]string, error)
}

type (
	GetRepositoryPluginFn func(ctx context.Context, typed runtime.Typed) (RepositoryPlugin, error)
	GetCredentialPluginFn func(ctx context.Context, typed runtime.Typed) (CredentialPlugin, error)
)
