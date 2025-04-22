package plugin

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const PluginCreator = "OCI Repository Plugin"

func init() {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		scheme,
		&Plugin{scheme: scheme, memory: inmemory.New()},
		&v1.OCIRepository{},
	); err != nil {
		panic(err)
	}
}

type Plugin struct {
	// embed empty base plugin to skip Ping method.
	contracts.EmptyBasePlugin

	scheme *runtime.Scheme
	memory cache.OCIDescriptorCache
}

func (p Plugin) AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Resource, error) {
	panic("implement me")
}

func clientCredentials(credentials contracts.Attributes) auth.Credential {
	cred := auth.Credential{}
	if username, ok := credentials["username"]; ok {
		cred.Username = string(username)
	}
	if password, ok := credentials["password"]; ok {
		cred.Password = string(password)
	}
	if refreshToken, ok := credentials["refresh_token"]; ok {
		cred.RefreshToken = string(refreshToken)
	}
	if accessToken, ok := credentials["access_token"]; ok {
		cred.AccessToken = string(accessToken)
	}
	return cred
}

func (p Plugin) AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
	// TODO implement me
	panic("implement me")
}

func (p Plugin) GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Descriptor, error) {
	repo, err := createRepository(request.Repository, credentials, p)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.GetComponentVersion(ctx, request.Name, request.Version)
}

func (p Plugin) GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
	// TODO implement me
	panic("implement me")
}

var (
	_ contracts.ReadOCMRepositoryPluginContract[*v1.OCIRepository]  = (*Plugin)(nil)
	_ contracts.WriteOCMRepositoryPluginContract[*v1.OCIRepository] = (*Plugin)(nil)
)

func createRepository(spec *v1.OCIRepository, credentials contracts.Attributes, p Plugin) (oci.ComponentVersionRepository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	urlResolver := urlresolver.New(urlString)
	urlResolver.SetClient(&auth.Client{
		Client: retry.DefaultClient,
		Header: map[string][]string{
			"User-Agent": {PluginCreator},
		},
		Credential: auth.StaticCredential(url.Host, clientCredentials(credentials)),
	})
	repo, err := oci.NewRepository(
		oci.WithResolver(urlResolver),
		oci.WithScheme(p.scheme),
		oci.WithCreator(PluginCreator),
		oci.WithOCIDescriptorCache(p.memory),
	)
	return repo, err
}
