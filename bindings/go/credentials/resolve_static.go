package credentials

import (
	"context"
	"maps"
	"slices"

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
	if creds, ok := s.staticCredentialsStore[identity.String()]; ok {
		return creds.DeepCopyTyped(), nil
	}
	// Fall back to matching like the credential graph: lookup identities can carry more
	// attributes than the stored keys (e.g. an OCI image's repository as path), so an
	// exact key lookup would miss entries scoped only to the registry.
	for _, key := range slices.Sorted(maps.Keys(s.staticCredentialsStore)) {
		stored, err := runtime.ParseIdentity(key)
		if err != nil {
			continue
		}
		if identity.Match(stored) {
			return s.staticCredentialsStore[key].DeepCopyTyped(), nil
		}
	}
	return nil, ErrNotFound
}
