package provider

import (
	"context"
	"log/slog"
	"sync"

	"oras.land/oras-go/v2/registry/remote/auth"

	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// cachedCredential represents a single credential entry in the cache,
// associating a runtime identity with its corresponding authentication credential.
type cachedCredential struct {
	identity   runtime.Identity
	credential auth.Credential
}

// credentialCache provides a thread-safe cache for repository credentials.
// It maintains a list of credentials indexed by repository identity (hostname and port).
// The cache supports multiple credential types including username/password,
// refresh tokens, and access tokens.
type credentialCache struct {
	mu          sync.RWMutex
	credentials []cachedCredential
}

// get retrieves credentials for a given hostport string.
// It performs a thread-safe lookup in the cache using the hostname and port
// to match against stored identities.
func (cache *credentialCache) get(_ context.Context, registry string) (auth.Credential, error) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	identity, err := runtime.ParseURLToIdentity(registry)
	if err != nil {
		return auth.EmptyCredential, err
	}

	for _, entry := range cache.credentials {
		if identity.Match(entry.identity, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			return entry.credential, nil
		}
	}
	return auth.EmptyCredential, nil
}

// add stores credentials for an OCI repository specification.
// If credentials already exist for the same identity, they will be overwritten
// if they are different from the new credentials.
func (cache *credentialCache) add(spec *ocirepospecv1.Repository, credentials map[string]string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	identity, err := runtime.ParseURLToIdentity(spec.BaseUrl)
	if err != nil {
		return err
	}

	newCredentials := toCredential(credentials)

	for i, entry := range cache.credentials {
		if identity.Match(entry.identity, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) && !equalCredentials(entry.credential, newCredentials) {
			slog.Warn("overwriting existing get for identity", slog.String("identity", identity.String()))
			cache.credentials[i].credential = newCredentials
			return nil
		}
	}

	cache.credentials = append(cache.credentials, cachedCredential{
		identity:   identity,
		credential: newCredentials,
	})

	return nil
}

// toCredential converts a map of credential key-value pairs into an auth.Credential.
func toCredential(credentials map[string]string) auth.Credential {
	cred := auth.Credential{}
	if username, ok := credentials["username"]; ok {
		cred.Username = username
	}
	if password, ok := credentials["password"]; ok {
		cred.Password = password
	}
	if refreshToken, ok := credentials["refresh_token"]; ok {
		cred.RefreshToken = refreshToken
	}
	if accessToken, ok := credentials["access_token"]; ok {
		cred.AccessToken = accessToken
	}
	return cred
}

// equalCredentials compares two auth.Credential instances for equality.
func equalCredentials(a, b auth.Credential) bool {
	return a.Username == b.Username &&
		a.Password == b.Password &&
		a.RefreshToken == b.RefreshToken &&
		a.AccessToken == b.AccessToken
}
