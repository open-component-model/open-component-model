package resource

import (
	"context"
	"fmt"
	"net/http"

	"oras.land/oras-go/v2/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	credidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options holds configuration options for the OCI resource repository.
type Options struct {
	// UserAgent is the User-Agent string to be used in HTTP requests by all the
	// repositories provided by the provider.
	UserAgent string

	// HTTPConfig is the HTTP client configuration (timeouts, per-host overrides)
	// used to build the repositories's internal HTTP client. When nil, default
	// transport timeouts and retry behaviour are used.
	// Accepts the serialisable config type so that external plugins can
	// round-trip it over the wire and reconstruct an equivalent client.
	HTTPConfig *httpv1alpha1.Config
}

type Option func(*Options)

// WithUserAgent sets the user agent option
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		o.UserAgent = userAgent
	}
}

// WithHTTPConfig sets the HTTP client configuration used for OCI registry
// traffic. The repository builds its internal client from cfg on construction,
// applying timeouts and per-host overrides.
// When nil, the default ocmhttp transport timeouts and retry behaviour are used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}

type ResourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
	userAgent        string
	httpClient       *http.Client
}

// make sure that ResourceRepository implements the oci ResourceRepository interface
var (
	_ repository.ResourceRepository       = (*ResourceRepository)(nil)
	_ repository.OwnershipAwareRepository = (*ResourceRepository)(nil)
)

func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	if options.UserAgent == "" {
		options.UserAgent = provider.DefaultCreator
	}

	return &ResourceRepository{
		filesystemConfig: filesystemConfig,
		userAgent:        options.UserAgent,
		httpClient: ocmhttp.New(
			ocmhttp.WithConfig(options.HTTPConfig),
			ocmhttp.WithUserAgent(options.UserAgent),
		),
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

func (p *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	repo, access, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return nil, err
	}
	resource = resource.DeepCopy()
	resource.Access = access
	resource, err = repo.ProcessResourceDigest(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("error processing resource digest: %w", err)
	}
	return resource, nil
}

func (p *ResourceRepository) getIdentity(obj runtime.Typed) (runtime.Identity, error) {
	baseURL, err := accessToBaseURL(obj)
	if err != nil {
		return nil, err
	}
	identity, err := runtime.ParseURLToIdentity(baseURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL to identity: %w", err)
	}
	identity.SetType(credidentityv1.Type)
	return identity, nil
}

func (p *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	repo, _, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return nil, err
	}
	b, err := repo.DownloadResource(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource: %w", err)
	}
	return b, nil
}

// AddOwnership attaches ownership information (i.e. the
// component name and version) to a resource. The ownership is attached as a
// referrer manifest pointing at the resource.
// Caution: EXPERIMENTAL
func (p *ResourceRepository) AddOwnership(ctx context.Context, component, version string, resource *descriptor.Resource, credentials runtime.Typed) error {
	repo, access, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return err
	}
	resource = resource.DeepCopy()
	resource.Access = access

	if err := repo.AddOwnership(ctx, component, version, resource, credentials); err != nil {
		return fmt.Errorf("error attaching ownership referrer: %w", err)
	}
	return nil
}

func (p *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	repo, access, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return nil, err
	}
	if err := validateUploadTarget(access); err != nil {
		return nil, err
	}
	b, err := repo.UploadResource(ctx, resource, content)
	if err != nil {
		return nil, fmt.Errorf("error uploading resource: %w", err)
	}
	return b, nil
}

func (p *ResourceRepository) getRepository(spec *ociv1.Repository, credentials *ocicredsv1.OCICredentials) (*oci.Repository, error) {
	repo, err := createRepository(spec, credentials, p.filesystemConfig, p.userAgent, p.httpClient)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo, nil
}

// accessToBaseURL derives the registry base URL from any access type this repository
// serves. Each carries an OCI reference, in a differently named field.
func accessToBaseURL(access runtime.Typed) (string, error) {
	var reference string
	var field string
	switch access := access.(type) {
	case *v1.OCIImage:
		reference, field = access.ImageReference, "imageReference"
	case *v1.OCIImageLayer:
		reference, field = access.Reference, "ref"
	default:
		return "", fmt.Errorf("unsupported access type %s: expected OCI image or OCI image layer", access.GetType())
	}
	if reference == "" {
		return "", fmt.Errorf("access type %s has an empty reference, set it in field %q", access.GetType(), field)
	}
	ref, err := looseref.ParseReference(reference)
	if err != nil {
		return "", fmt.Errorf("error parsing loose image reference %q: %w", reference, err)
	}
	// host is the registry with sane defaulting
	return ref.RegistryWithScheme(), nil
}

// validateUploadTarget rejects access types that can be read but not written to.
func validateUploadTarget(access runtime.Typed) error {
	if _, ok := access.(*v1.OCIImageLayer); ok {
		return fmt.Errorf("unsupported access type %s as upload target: expected OCI image", access.GetType())
	}
	return nil
}

func createRepository(
	spec *ociv1.Repository,
	credentials *ocicredsv1.OCICredentials,
	filesystemConfig *filesystemv1alpha1.Config,
	userAgent string,
	httpClient *http.Client,
) (*oci.Repository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	urlResolver, err := urlresolver.New(
		urlresolver.WithBaseURL(urlString),
		urlresolver.WithBaseClient(&auth.Client{
			Client: httpClient,
			Header: map[string][]string{
				"User-Agent": {userAgent},
			},
			Credential: auth.StaticCredential(url.Host, ocicredentials.MapCredentials(credentials)),
		}))
	if err != nil {
		return nil, fmt.Errorf("failed to create URL resolver: %w", err)
	}
	var tempDir string
	if filesystemConfig != nil && filesystemConfig.TempFolder != nil {
		tempDir = *filesystemConfig.TempFolder
	}
	options := []oci.RepositoryOption{
		oci.WithResolver(urlResolver),
		oci.WithCreator(userAgent),
		oci.WithTempDir(tempDir), // the filesystem config being empty is a valid config
	}

	repo, err := oci.NewRepository(options...)
	return repo, err
}

var _ ocistream.ResourceRepository = (*ResourceRepository)(nil)

// resolveRepository resolves the inner *oci.Repository for the given resource access and
// credentials. It also returns the access materialized into its registered Go type, which
// callers hand on to the inner repository so its type switch can match it.
func (p *ResourceRepository) resolveRepository(resource *descriptor.Resource, credentials runtime.Typed) (*oci.Repository, runtime.Typed, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	baseURL, err := accessToBaseURL(obj)
	if err != nil {
		return nil, nil, err
	}

	var ociCredentials *ocicredsv1.OCICredentials
	if credentials != nil {
		ociCredentials, err = ocicredsv1.ConvertToOCICredentials(credentials)
		if err != nil {
			return nil, nil, fmt.Errorf("error converting credentials: %w", err)
		}
	}
	repo, err := p.getRepository(&ociv1.Repository{BaseUrl: baseURL}, ociCredentials)
	if err != nil {
		return nil, nil, err
	}
	return repo, obj, nil
}

func (p *ResourceRepository) DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (ocistream.ResourceStream, error) {
	repo, _, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return nil, err
	}
	stream, err := repo.DownloadResourceStream(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("error creating resource stream: %w", err)
	}
	return stream, nil
}

func (p *ResourceRepository) UploadResourceStream(ctx context.Context, resource *descriptor.Resource, stream ocistream.ResourceStream, credentials runtime.Typed) (*descriptor.Resource, error) {
	repo, access, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return nil, err
	}
	if err := validateUploadTarget(access); err != nil {
		return nil, err
	}
	res, err := repo.UploadResourceStream(ctx, resource, stream)
	if err != nil {
		return nil, fmt.Errorf("error streaming resource upload: %w", err)
	}
	return res, nil
}
