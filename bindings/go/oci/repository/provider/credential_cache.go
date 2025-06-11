package provider

import (
	"context"
	"log/slog"
	"net"
	"net/url"
	"sync"

	"oras.land/oras-go/v2/registry/remote/auth"

	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type cachedCredential struct {
	identity   runtime.Identity
	credential auth.Credential
}
type credentialCache struct {
	mu          sync.RWMutex
	credentials []cachedCredential
}

func (cache *credentialCache) get(_ context.Context, hostport string) (auth.Credential, error) {
	cache.mu.RLock()
	cache.mu.RUnlock()

	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return auth.EmptyCredential, err
	}
	identity := runtime.Identity{
		runtime.IdentityAttributeHostname: host,
		runtime.IdentityAttributePort:     port,
	}

	for _, entry := range cache.credentials {
		if identity.Match(entry.identity, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			return entry.credential, nil
		}
	}
	return auth.EmptyCredential, nil
}

func (cache *credentialCache) add(spec *ocirepospecv1.Repository, credentials map[string]string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	parsedBaseURL, err := url.Parse(spec.BaseUrl)
	if err != nil {
		return err
	}

	hostname, port := parsedBaseURL.Hostname(), parsedBaseURL.Port()

	identity := runtime.Identity{
		runtime.IdentityAttributeHostname: hostname,
		runtime.IdentityAttributePort:     port,
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

func equalCredentials(a, b auth.Credential) bool {
	return a.Username == b.Username &&
		a.Password == b.Password &&
		a.RefreshToken == b.RefreshToken &&
		a.AccessToken == b.AccessToken
}
