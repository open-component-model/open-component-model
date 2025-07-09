package plugin

import (
	"fmt"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Creator = "Builtin OCI Repository Plugin"

type Repository interface {
	oci.ResourceRepository
	oci.ComponentVersionRepository
}

func createRepository(
	spec *ociv1.Repository,
	credentials map[string]string,
	manifests cache.OCIDescriptorCache,
	layers cache.OCIDescriptorCache,
) (Repository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	urlResolver, err := urlresolver.New(
		urlresolver.WithBaseURL(urlString),
		urlresolver.WithBaseClient(&auth.Client{
			Client: retry.DefaultClient,
			Header: map[string][]string{
				"User-Agent": {Creator},
			},
			Credential: auth.StaticCredential(url.Host, clientCredentials(credentials)),
		}))
	if err != nil {
		return nil, fmt.Errorf("failed to create URL resolver: %w", err)
	}
	repo, err := oci.NewRepository(
		oci.WithResolver(urlResolver),
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
