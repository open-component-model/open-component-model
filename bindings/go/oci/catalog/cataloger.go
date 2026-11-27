package catalog

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
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

// CatalogerOptions carries everything NewDistributionCataloger needs to build a
// cataloger.
type CatalogerOptions struct {
	// Host is the registry host, for example "ghcr.io".
	Host string
	// HTTPClient is the shared OCM HTTP client.
	HTTPClient *http.Client
	// AuthClient is the ORAS auth client for the distribution token flow. It
	// is already bound to the registry credentials.
	AuthClient *auth.Client
	// PlainHTTP signals that the host is reached over cleartext HTTP.
	PlainHTTP bool
	// Logger is the logger the cataloger uses. It may be nil.
	Logger *slog.Logger
}
