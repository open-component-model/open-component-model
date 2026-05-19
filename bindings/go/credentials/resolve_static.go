package credentials

import (
	"context"
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StaticCredentialsResolver is a simple implementation of the Resolver interface
// that uses a static map to store credentials.
type StaticCredentialsResolver struct {
	staticCredentialsStore map[string]runtime.Typed
}

// NewStaticCredentialsResolver creates a new StaticCredentialsResolver with the provided credentials map.
// The input map should have keys that can be derived from the string representation of runtime.Identity
// and values that are maps of credential key-value pairs.
func NewStaticCredentialsResolver(credMap map[string]map[string]string) *StaticCredentialsResolver {
	credsStore := map[string]runtime.Typed{}

	for k, v := range credMap {
		credsStore[k] = &v1.DirectCredentials{
			Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
			Properties: maps.Clone(v),
		}
	}

	return &StaticCredentialsResolver{
		staticCredentialsStore: credsStore,
	}
}

// NewStaticCredentialsResolverFromTyped creates a new StaticCredentialsResolver with the provided credentials.
// The input map should have keys that can be derived from the string representation of runtime.Identity
// and values that are runtime.Typed credentials.
// TODO(matthiasbruns): Update tests to use NewStaticCredentialsResolverFromTyped instead of NewStaticCredentialsResolver
func NewStaticCredentialsResolverFromTyped(credMap map[string]runtime.Typed) *StaticCredentialsResolver {
	credStore := maps.Clone(credMap)

	return &StaticCredentialsResolver{
		staticCredentialsStore: credStore,
	}
}

func (s *StaticCredentialsResolver) Resolve(_ context.Context, identity runtime.Identity) (runtime.Typed, error) {
	creds, ok := s.staticCredentialsStore[identity.String()]
	if !ok {
		return nil, ErrNotFound
	}
	// return cloned creds
	return creds.DeepCopyTyped(), nil
}
