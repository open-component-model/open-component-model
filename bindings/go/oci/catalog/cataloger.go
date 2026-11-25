package catalog

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RepositoryCataloger yields raw repository names for a registry. It uses
// whatever catalog API the backend exposes. The signature mirrors ORAS
// remote.Registry.Repositories and the ComponentLister callback contract.
type RepositoryCataloger interface {
	// Repositories lists repository names, paginated. last is the last
	// repository name of the previous page. An empty last starts from the
	// top. fn is called once per page. An implementation that cannot
	// paginate calls fn one time with the full list and ignores last.
	Repositories(ctx context.Context, last string, fn func(repos []string) error) error
}

// CatalogerOptions carries everything a factory needs to build a cataloger.
type CatalogerOptions struct {
	// Host is the registry host, for example "ghcr.io" or
	// "<account>.dkr.ecr.<region>.amazonaws.com".
	Host string
	// SubPath is the OCM repository prefix (ociv1.Repository.SubPath). It may
	// be empty.
	SubPath string
	// HTTPClient is the shared OCM HTTP client.
	HTTPClient *http.Client
	// AuthClient is the ORAS auth client for the distribution token flow. It
	// is already bound to the registry credentials.
	AuthClient *auth.Client
	// Credentials holds the raw credentials for APIs that need the username
	// and password directly. It may be nil.
	Credentials *credsv1.OCICredentials
	// Identity is the consumer identity for the host.
	Identity runtime.Identity
	// PlainHTTP signals that the host is reached over cleartext HTTP.
	PlainHTTP bool
	// Logger is the logger the cataloger uses. It may be nil.
	Logger *slog.Logger
}

// CatalogerFactory builds a RepositoryCataloger for hosts it recognizes.
type CatalogerFactory interface {
	// Match reports whether this factory handles the given registry host.
	Match(host string) bool
	// New builds a RepositoryCataloger from the given options.
	New(opts CatalogerOptions) (RepositoryCataloger, error)
}

// registry holds the registered factories. A per-registry file adds its
// factory from init(), so no shared file is edited when a registry is added.
var registry = struct {
	mu        sync.RWMutex
	factories []CatalogerFactory
}{}

// RegisterCataloger adds a factory to the registry. Each per-registry file
// calls this from init(), so adding a registry needs no edit to a shared file.
func RegisterCataloger(f CatalogerFactory) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.factories = append(registry.factories, f)
}

// CatalogerFor returns the cataloger of the first factory that matches the
// host. If no factory matches, it returns the standard /v2/_catalog
// cataloger built by NewDistributionCataloger.
func CatalogerFor(opts CatalogerOptions) (RepositoryCataloger, error) {
	registry.mu.RLock()
	factories := registry.factories
	registry.mu.RUnlock()

	for _, f := range factories {
		if f.Match(opts.Host) {
			return f.New(opts)
		}
	}
	return NewDistributionCataloger(opts)
}
