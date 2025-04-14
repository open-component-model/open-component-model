package static

import (
	"context"
	"encoding/json"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CredentialPlugin struct {
	ConsumerIdentityTypeAttributes map[runtime.Type]map[string]func(v any) (string, string)
	CredentialFunc                 func(ctx context.Context, identity runtime.Identity, credentials map[string]string) (resolved map[string]string, err error)
}

var _ credentials.CredentialPlugin = CredentialPlugin{}

func (p CredentialPlugin) GetConsumerIdentity(_ context.Context, typed runtime.Typed) (runtime.Identity, error) {
	attrs, ok := p.ConsumerIdentityTypeAttributes[typed.GetType()]
	if !ok {
		return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
	}

	data, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}

	mm := make(map[string]interface{})
	if err := json.Unmarshal(data, &mm); err != nil {
		return nil, err
	}

	identity := make(runtime.Identity)
	identity[runtime.IdentityAttributeType] = typed.GetType().String()
	for k, attr := range attrs {
		if val, ok := mm[k]; ok {
			newKey, newVal := attr(val)
			identity[newKey] = newVal
		}
	}

	return identity, nil
}

func (p CredentialPlugin) Resolve(ctx context.Context, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	return p.CredentialFunc(ctx, identity, credentials)
}

type RepositoryPlugin struct {
	RepositoryConfigTypes  []runtime.Type
	RepositoryIdentityFunc func(config runtime.Typed) (runtime.Identity, error)
	ResolveFunc            func(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error)
}

var _ credentials.RepositoryPlugin = RepositoryPlugin{}

func (s RepositoryPlugin) SupportedRepositoryConfigTypes() []runtime.Type {
	return s.RepositoryConfigTypes
}

func (s RepositoryPlugin) ConsumerIdentityForConfig(_ context.Context, config runtime.Typed) (runtime.Identity, error) {
	return s.RepositoryIdentityFunc(config)
}

func (s RepositoryPlugin) Resolve(ctx context.Context, config runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	return s.ResolveFunc(ctx, config, identity, credentials)
}
