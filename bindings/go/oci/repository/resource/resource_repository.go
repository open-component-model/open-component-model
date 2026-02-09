package resource

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options holds configuration options for the OCI resource repository.
type Options struct {
	// UserAgent is the User-Agent string to be used in HTTP requests by all the
	// repositories provided by the provider.
	UserAgent string
}

type Option func(*Options)

// WithUserAgent sets the user agent option
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		o.UserAgent = userAgent
	}
}

type ResourceRepository struct {
	manifests        cache.OCIDescriptorCache
	layers           cache.OCIDescriptorCache
	filesystemConfig *filesystemv1alpha1.Config
	userAgent        string
}

// make sure that ResourceRepository implements the oci ResourceRepository interface
var _ repository.ResourceRepository = (*ResourceRepository)(nil)

func NewResourceRepository(manifests, layers cache.OCIDescriptorCache, filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	if options.UserAgent == "" {
		options.UserAgent = provider.DefaultCreator
	}

	return &ResourceRepository{
		manifests:        manifests,
		layers:           layers,
		filesystemConfig: filesystemConfig,
		userAgent:        options.UserAgent,
	}
}

func (p *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return ociaccess.Scheme
}

func (p *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	return p.getIdentity(obj)
}

func (p *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	return p.getIdentity(obj)
}

func (p *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}

		repo, err := p.getRepository(&ociv1.Repository{
			BaseUrl: baseURL,
		}, credentials)
		if err != nil {
			return nil, fmt.Errorf("error creating repository: %w", err)
		}

		resource = resource.DeepCopy()
		resource.Access = access

		resource, err := repo.ProcessResourceDigest(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error downloading resource: %w", err)
		}

		return resource, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for downloading the resource", t)
	}
}

func (p *ResourceRepository) getIdentity(obj runtime.Typed) (runtime.Identity, error) {
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}
		identity, err := runtime.ParseURLToIdentity(baseURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing URL to identity: %w", err)
		}
		identity.SetType(runtime.NewUnversionedType(ociv1.Type))
		return identity, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for getting identity", obj.GetType())
	}
}

func (p *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}

		repo, err := p.getRepository(&ociv1.Repository{
			BaseUrl: baseURL,
		}, credentials)
		if err != nil {
			return nil, fmt.Errorf("error creating repository: %w", err)
		}

		b, err := repo.DownloadResource(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error downloading resource: %w", err)
		}

		return b, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for downloading the resource", t)
	}
}

func (p *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}

		repo, err := p.getRepository(&ociv1.Repository{
			BaseUrl: baseURL,
		}, credentials)
		if err != nil {
			return nil, fmt.Errorf("error creating repository: %w", err)
		}

		b, err := repo.UploadResource(ctx, resource, content)
		if err != nil {
			return nil, fmt.Errorf("error uploading resource: %w", err)
		}

		return b, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for uploading the resource", t)
	}
}

func (p *ResourceRepository) getRepository(spec *ociv1.Repository, creds map[string]string) (*oci.Repository, error) {
	repo, err := createRepository(spec, creds, p.manifests, p.layers, p.filesystemConfig, p.userAgent)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo, nil
}

func ociImageAccessToBaseURL(access *v1.OCIImage) (string, error) {
	ref, err := looseref.ParseReference(access.ImageReference)
	if err != nil {
		return "", fmt.Errorf("error parsing loose image reference %q: %w", access.ImageReference, err)
	}
	// host is the registry with sane defaulting
	baseURL := ref.RegistryWithScheme()
	return baseURL, nil
}

func createRepository(
	spec *ociv1.Repository,
	credentials map[string]string,
	manifests cache.OCIDescriptorCache,
	layers cache.OCIDescriptorCache,
	filesystemConfig *filesystemv1alpha1.Config,
	userAgent string,
) (*oci.Repository, error) {
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
				"User-Agent": {userAgent},
			},
			Credential: auth.StaticCredential(url.Host, clientCredentials(credentials)),
		}))
	if err != nil {
		return nil, fmt.Errorf("failed to create URL resolver: %w", err)
	}
	tempDir := ""
	if filesystemConfig != nil {
		tempDir = filesystemConfig.TempFolder
	}
	options := []oci.RepositoryOption{
		oci.WithResolver(urlResolver),
		oci.WithCreator(userAgent),
		oci.WithManifestCache(manifests),
		oci.WithLayerCache(layers),
		oci.WithTempDir(tempDir), // the filesystem config being empty is a valid config
	}

	repo, err := oci.NewRepository(options...)
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
