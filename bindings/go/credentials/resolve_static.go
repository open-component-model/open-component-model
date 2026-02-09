package credentials

import (
	"context"

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
	credStore := make(map[string]map[string]string)

	for id, creds := range credMap {
		copiedCreds := make(map[string]string)
		for k, v := range creds {
			copiedCreds[k] = v
		}
		credStore[id] = copiedCreds
	}

	return &StaticCredentialsResolver{
		staticCredentialsStore: credStore,
	}
}

func (s *StaticCredentialsResolver) Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	creds, ok := s.staticCredentialsStore[identity.String()]
	if !ok {
		return nil, ErrNotFound
	}

	// clone the credentials map to prevent external modification
	clonedCreds := make(map[string]string)
	for k, v := range creds {
		clonedCreds[k] = v
	}

	return clonedCreds, nil
}
