package credentials

import (
	"context"
	"sync"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type (
	credentials   map[string]string
	credentialsID string
)

// StaticCredentialsResolver is a simple implementation of the Resolver interface
// that uses a static map to store credentials.
type StaticCredentialsResolver struct {
	staticCredentialsStore map[credentialsID]credentials
	mutex                  *sync.Mutex
}

// NewStaticCredentialsResolver creates a new StaticCredentialsResolver with the provided credentials map.
// The input map should have keys that can be derived from the string representation of runtime.Identity
// and values that are maps of credential key-value pairs.
func NewStaticCredentialsResolver(credMap map[string]map[string]string) *StaticCredentialsResolver {
	credStore := make(map[credentialsID]credentials)

	for id, creds := range credMap {
		credStore[credentialsID(id)] = creds
	}

	return &StaticCredentialsResolver{
		staticCredentialsStore: credStore,
		mutex:                  &sync.Mutex{},
	}
}

func (s *StaticCredentialsResolver) Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	creds, ok := s.staticCredentialsStore[credentialsID(identity.String())]
	if !ok {
		return nil, ErrNotFound
	}
	return creds, nil
}
