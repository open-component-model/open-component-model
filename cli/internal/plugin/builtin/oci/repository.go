package plugin

import (
	"fmt"
	"sync"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Creator = "OCI Repository ComponentVersionRepositoryPlugin"

type Repository interface {
	oci.ResourceRepository
	oci.ComponentVersionRepository
}

func createRepository(
	spec *ociv1.Repository,
	credentials map[string]string,
	scheme *runtime.Scheme,
	manifests cache.OCIDescriptorCache,
	layers cache.OCIDescriptorCache,
) (Repository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	urlResolver := urlresolver.New(urlString)
	urlResolver.SetClient(&auth.Client{
		Client: retry.DefaultClient,
		Header: map[string][]string{
			"User-Agent": {Creator},
		},
		Credential: auth.StaticCredential(url.Host, clientCredentials(credentials)),
	})
	repo, err := oci.NewRepository(
		oci.WithResolver(urlResolver),
		oci.WithScheme(scheme),
		oci.WithCreator(Creator),
		oci.WithManifestCache(manifests),
		oci.WithLayerCache(layers),
	)
	return repo, err
}

func clientCredentials(credentials map[string]string) auth.Credential {
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

type repoCache struct {
	mu    sync.RWMutex
	cache map[string]Repository
}

func newRepoCache() *repoCache {
	return &repoCache{
		cache: make(map[string]Repository),
	}
}

func (c *repoCache) Get(key string) (Repository, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	repo, exists := c.cache[key]
	return repo, exists
}

func (c *repoCache) Set(key string, repo Repository) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = repo
}
