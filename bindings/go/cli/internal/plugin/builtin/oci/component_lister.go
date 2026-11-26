package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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

// httpPrefixMigrationGuide explains how to replace a scheme prefix on a
// registry URL with plainHTTP in the HTTP client configuration. It matches the
// hint the OCI resolver gives, so both paths point the user at the same place.
const httpPrefixMigrationGuide = "https://ocm.software/docs/changelog/migrate-from-http-prefixes/"

// NewComponentListerPlugin creates an OCIComponentListerPlugin. httpConfig and
// logger may be nil. The shared HTTP client used for remote registries is
// built from httpConfig.
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
// *ociv1.Repository (remote OCI registry). A raw specification is converted
// through the scheme first, so a spec that arrives as *runtime.Raw (from
// configuration or a resolver) is handled like the typed one.
func (l *OCIComponentListerPlugin) GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, creds runtime.Typed) (repository.ComponentLister, error) {
	spec, err := getConvertedTypedSpec(repositorySpecification)
	if err != nil {
		return nil, err
	}

	switch spec := spec.(type) {
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

	// A scheme prefix on the base URL is deprecated input: it never changes the
	// connection. Warn and point at the migration guide, exactly as the OCI
	// resolver does, so the lister and the resolver behave the same way.
	if parsed.Scheme != "" {
		slog.WarnContext(ctx, "the scheme prefix on a registry URL is deprecated; drop it and set plainHTTP in the HTTP client configuration for hosts without TLS",
			slog.String("host", host), slog.String("scheme", parsed.Scheme), slog.String("url", httpPrefixMigrationGuide))
	}

	plainHTTP, err := ocmhttp.PlainHTTPFor(ctx, l.httpConfig, host, parsed.Scheme, false)
	if err != nil {
		return nil, fmt.Errorf("%w and drop the prefix (see %s)", err, httpPrefixMigrationGuide)
	}

	cataloger, err := catalog.NewDistributionCataloger(catalog.CatalogerOptions{
		Host:       host,
		HTTPClient: l.httpClient,
		AuthClient: ocicreds.AuthClient(l.httpClient, identity, ociCredentials, userAgent),
		PlainHTTP:  plainHTTP,
		Logger:     l.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating cataloger for host %q: %w", host, err)
	}

	return catalog.NewComponentLister(cataloger, subPath), nil
}

// GetComponentListerCredentialConsumerIdentity retrieves the consumer identity
// for the given repository specification. Remote OCI registries resolve to an
// identity usable for credential lookup. CTF repositories do not need
// credentials and return an error. A raw specification is converted through the
// scheme first.
func (l *OCIComponentListerPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	spec, err := getConvertedTypedSpec(repositorySpecification)
	if err != nil {
		return nil, err
	}

	switch spec := spec.(type) {
	case *ociv1.Repository:
		return identityv1.IdentityFromOCIRepository(spec)
	case *ctfv1.Repository:
		return nil, errors.Join(ErrWrongUsage, errors.New("credentials not supported for CTF repositories"))
	default:
		return nil, errors.Join(ErrWrongUsage, fmt.Errorf("unsupported repository type: %T", repositorySpecification))
	}
}

// getConvertedTypedSpec converts any repository specification (including a
// *runtime.Raw) to its typed object in the OCI repository scheme, so callers
// can switch on the concrete type. It mirrors the conversion the OCI component
// version provider does.
func getConvertedTypedSpec(repositorySpecification runtime.Typed) (runtime.Typed, error) {
	spec := repositorySpecification.DeepCopyTyped()
	_, _ = ocirepository.Scheme.DefaultType(spec)
	obj, err := ocirepository.Scheme.NewObject(spec.GetType())
	if err != nil {
		return nil, errors.Join(ErrWrongUsage, fmt.Errorf("unsupported repository type %q: %w", spec.GetType(), err))
	}
	if err := ocirepository.Scheme.Convert(spec, obj); err != nil {
		return nil, errors.Join(ErrWrongUsage, fmt.Errorf("could not convert repository specification: %w", err))
	}
	return obj, nil
}
