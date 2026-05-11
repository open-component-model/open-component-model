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
	staticCredentialsStore map[string]map[string]string
}

// NewStaticCredentialsResolver creates a new StaticCredentialsResolver with the provided credentials map.
// The input map should have keys that can be derived from the string representation of runtime.Identity
// and values that are maps of credential key-value pairs.
func NewStaticCredentialsResolver(credMap map[string]map[string]string) *StaticCredentialsResolver {
	credStore := maps.Clone(credMap)

	return &StaticCredentialsResolver{
		staticCredentialsStore: credStore,
	}
}

func (s *StaticCredentialsResolver) Resolve(_ context.Context, identity runtime.Identity) (map[string]string, error) {
	creds, ok := s.staticCredentialsStore[identity.String()]
	if !ok {
		return nil, ErrNotFound
	}

	return maps.Clone(creds), nil
}

func (s *StaticCredentialsResolver) ResolveTyped(_ context.Context, identity runtime.Identity) (runtime.Typed, error) {
	creds, ok := s.staticCredentialsStore[identity.String()]
	if !ok {
		return nil, ErrNotFound
	}
	return &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: maps.Clone(creds),
	}, nil
}
