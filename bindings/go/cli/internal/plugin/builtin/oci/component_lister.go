package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/ctf"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/catalog"
	ocicreds "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIComponentListerPlugin is a built-in CLI plug-in that lists OCM components
// stored in an OCI repository. One plug-in serves the whole
// ocirepository.Scheme: it lists components from both CTF archives
// (*ctfv1.Repository) and remote OCI registries (*ociv1.Repository).
type OCIComponentListerPlugin struct {
	// httpConfig is the central HTTP client configuration. It decides the
	// per-host plainHTTP setting for remote registries. It may be nil.
	httpConfig *httpv1alpha1.Config
	// httpClient is the shared HTTP client used for remote registry requests.
	httpClient *http.Client
	// logger is passed to the remote cataloger. It may be nil.
	logger *slog.Logger
}

var _ componentlister.InternalComponentListerPluginContract = (*OCIComponentListerPlugin)(nil)

var ErrWrongUsage = errors.New("wrong usage of OCI component lister plugin")

// userAgent identifies this plug-in on authenticated OCI requests.
const userAgent = "Builtin OCI Component Lister Plugin"

// NewComponentListerPlugin creates an OCIComponentListerPlugin. httpConfig and
// logger may be nil. httpClient must not be nil for remote OCI registries; it
// is built from httpConfig when not provided.
func NewComponentListerPlugin(httpConfig *httpv1alpha1.Config, logger *slog.Logger) *OCIComponentListerPlugin {
	return &OCIComponentListerPlugin{
		httpConfig: httpConfig,
		httpClient: ocmhttp.New(
			ocmhttp.WithConfig(httpConfig),
			ocmhttp.WithUserAgent(userAgent),
		),
		logger: logger,
	}
}

func (l *OCIComponentListerPlugin) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return ocirepository.Scheme
}

// GetComponentLister returns a component lister for the given repository
// specification. It supports *ctfv1.Repository (CTF archive) and
// *ociv1.Repository (remote OCI registry).
func (l *OCIComponentListerPlugin) GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, creds runtime.Typed) (repository.ComponentLister, error) {
	switch spec := repositorySpecification.(type) {
	case *ctfv1.Repository:
		archive, _, err := ctf.OpenCTFByFileExtension(ctx, ctf.OpenCTFOptions{
			Path: spec.FilePath,
			Flag: ctf.O_RDONLY,
		})
		if err != nil {
			return nil, fmt.Errorf("error opening CTF archive: %w", err)
		}
		return ocictf.NewComponentLister(archive), nil
	case *ociv1.Repository:
		return l.remoteComponentLister(ctx, spec, creds)
	default:
		return nil, errors.Join(ErrWrongUsage, fmt.Errorf("unsupported repository type: %T", repositorySpecification))
	}
}

// remoteComponentLister builds a ComponentLister for a remote OCI registry. It
// mirrors the credential and auth wiring of the OCI repository provider.
func (l *OCIComponentListerPlugin) remoteComponentLister(ctx context.Context, spec *ociv1.Repository, creds runtime.Typed) (repository.ComponentLister, error) {
	identity, err := identityv1.OCIRegistryIdentityFromOCIRepository(spec)
	if err != nil {
		return nil, err
	}

	var ociCredentials *credsv1.OCICredentials
	if creds != nil {
		ociCredentials, err = credsv1.ConvertToOCICredentials(creds)
		if err != nil {
			return nil, fmt.Errorf("error converting credentials: %w", err)
		}
	}

	parsed, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	host := parsed.Host

	// When SubPath is empty and BaseUrl embeds a path (for example
	// "ghcr.io/open-component-model/ocm"), the path is the OCM repository
	// prefix. This mirrors the auto-extraction documented on ociv1.Repository
	// and keeps the component-descriptors/ prefix correct, so listing does not
	// silently return an empty result.
	subPath := spec.SubPath
	if subPath == "" {
		subPath = strings.Trim(parsed.Path, "/")
	}

	plainHTTP, err := ocmhttp.PlainHTTPFor(ctx, l.httpConfig, host, parsed.Scheme, false)
	if err != nil {
		return nil, fmt.Errorf("failed to determine plain HTTP setting for host %q: %w", host, err)
	}

	header := http.Header{"User-Agent": {userAgent}}
	authClient := &auth.Client{
		Client:         l.httpClient,
		Cache:          auth.NewCache(),
		CredentialFunc: ocicreds.CredentialFunc(identity, ociCredentials),
		Header:         header,
		// Keep the distribution spec token flow for basic credentials instead
		// of attempting OAuth2 first, which not all registries implement.
		TokenFetcher: auth.NewCompositeTokenFetcher(l.httpClient, header, "", true),
	}

	consumerIdentity, err := identityv1.IdentityFromOCIRepository(spec)
	if err != nil {
		return nil, err
	}

	cataloger, err := catalog.CatalogerFor(catalog.CatalogerOptions{
		Host:        host,
		SubPath:     subPath,
		HTTPClient:  l.httpClient,
		AuthClient:  authClient,
		Credentials: ociCredentials,
		Identity:    consumerIdentity,
		PlainHTTP:   plainHTTP,
		Logger:      l.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating cataloger for host %q: %w", host, err)
	}

	return catalog.NewComponentLister(cataloger, subPath), nil
}

// GetComponentListerCredentialConsumerIdentity retrieves the consumer identity
// for the given repository specification. Remote OCI registries resolve to an
// identity usable for credential lookup. CTF repositories do not need
// credentials and return an error.
func (l *OCIComponentListerPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	switch spec := repositorySpecification.(type) {
	case *ociv1.Repository:
		return identityv1.IdentityFromOCIRepository(spec)
	case *ctfv1.Repository:
		return nil, errors.Join(ErrWrongUsage, errors.New("credentials not supported for CTF repositories"))
	default:
		return nil, errors.Join(ErrWrongUsage, fmt.Errorf("unsupported repository type: %T", repositorySpecification))
	}
}
