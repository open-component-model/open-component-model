package resource

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	remotecredentials "github.com/oras-project/oras-go/v3/registry/remote/credentials"

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
	ocicreds "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
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
	// httpConfig is what httpClient was built from; the URL resolver reads
	// its per-host plainHTTP entries.
	httpConfig *httpv1alpha1.Config
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
		httpConfig: options.HTTPConfig,
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
	if err := rejectReadOnlyAccess(access); err != nil {
		return nil, err
	}
	b, err := repo.UploadResource(ctx, resource, content)
	if err != nil {
		return nil, fmt.Errorf("error uploading resource: %w", err)
	}
	return b, nil
}

func (p *ResourceRepository) getRepository(spec *ociv1.Repository, credentials *ocicredsv1.OCICredentials) (*oci.Repository, error) {
	repo, err := createRepository(spec, credentials, p.filesystemConfig, p.userAgent, p.httpClient, p.httpConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo, nil
}

func accessToBaseURL(access runtime.Typed) (string, error) {
	switch access := access.(type) {
	case *v1.OCIImage:
		return ociImageAccessToBaseURL(access)
	case *v1.OCIImageLayer:
		return ociImageLayerAccessToBaseURL(access)
	default:
		return "", fmt.Errorf("unsupported access type %s: expected OCI image or OCI image layer", access.GetType())
	}
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

func ociImageLayerAccessToBaseURL(access *v1.OCIImageLayer) (string, error) {
	if access.Reference == "" {
		return "", fmt.Errorf("access type %s has an empty reference, set it in field %q", access.GetType(), "ref")
	}
	ref, err := looseref.ParseReference(access.Reference)
	if err != nil {
		return "", fmt.Errorf("error parsing loose image reference %q: %w", access.Reference, err)
	}
	if ref.Registry == "" {
		return "", fmt.Errorf("reference %q in field %q must include a registry", access.Reference, "ref")
	}
	return ref.RegistryWithScheme(), nil
}

// rejectReadOnlyAccess rejects access types that can be read but not written to.
func rejectReadOnlyAccess(access runtime.Typed) error {
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
	httpConfig *httpv1alpha1.Config,
) (*oci.Repository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	header := http.Header{"User-Agent": {userAgent}}
	resolverOpts := []urlresolver.Option{
		urlresolver.WithBaseURL(urlString),
		urlresolver.WithBaseURLScheme(url.Scheme),
		urlresolver.WithBaseClient(&auth.Client{
			Client:         httpClient,
			Header:         header,
			CredentialFunc: credentialFunc(url.Host, credentials),
			// Keep the distribution spec token flow for basic credentials instead of
			// attempting OAuth2 first, which not all registries implement.
			TokenFetcher: auth.NewCompositeTokenFetcher(httpClient, header, "", true),
		}),
	}
	if httpConfig != nil {
		resolverOpts = append(resolverOpts, urlresolver.WithHTTPConfig(httpConfig))
	}
	urlResolver, err := urlresolver.New(resolverOpts...)
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
	if err := rejectReadOnlyAccess(access); err != nil {
		return nil, err
	}
	res, err := repo.UploadResourceStream(ctx, resource, stream)
	if err != nil {
		return nil, fmt.Errorf("error streaming resource upload: %w", err)
	}
	return res, nil
}

// GetCredentialTypeScheme returns the credential payload types this repository consumes
// (currently OCICredentials/v1). ocicreds.Scheme must NOT be used here: it holds the
// credential repository configuration types (DockerConfig) instead of payload types.
func (p *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return ocicreds.CredentialTypeScheme
}

// credentialFunc returns credentials for requests to host. oras matches on the
// canonical, lower-cased registry name while host keeps the case written in the
// base URL, so it is lower-cased here as host names are case-insensitive.
func credentialFunc(host string, credentials *ocicredsv1.OCICredentials) remotecredentials.CredentialFunc {
	return remotecredentials.StaticCredentialFunc(strings.ToLower(host), ocicredentials.MapCredentials(credentials))
}
