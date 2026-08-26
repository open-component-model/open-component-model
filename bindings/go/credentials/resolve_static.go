package credentials

import (
	"context"
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StaticCredentialsResolver is a simple in-memory Resolver backed by a static map.
// Keys must be the string representation of runtime.Identity (see runtime.Identity.String).
type StaticCredentialsResolver struct {
	staticCredentialsStore map[string]runtime.Typed
}

func NewStaticCredentialsResolver(credMap map[string]map[string]string) *StaticCredentialsResolver {
	store := make(map[string]runtime.Typed, len(credMap))

	for k, v := range credMap {
		store[k] = &v1.DirectCredentials{
			Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
			Properties: maps.Clone(v),
		}
	}

	return &StaticCredentialsResolver{
		staticCredentialsStore: store,
	}
}

func NewStaticTypedCredentialsResolver(credMap map[string]runtime.Typed) *StaticCredentialsResolver {
	store := make(map[string]runtime.Typed, len(credMap))
	for k, v := range credMap {
		store[k] = v.DeepCopyTyped()
	}
	return &StaticCredentialsResolver{
		staticCredentialsStore: store,
	}
}

func (s *StaticCredentialsResolver) Resolve(_ context.Context, identity runtime.Identity) (runtime.Typed, error) {
	creds, ok := s.staticCredentialsStore[identity.String()]
	if !ok {
		return nil, ErrNotFound
	}
	return creds.DeepCopyTyped(), nil
}
