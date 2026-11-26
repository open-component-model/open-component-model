package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/oras-project/oras-go/v3/registry/remote/errcode"
)

// ErrCatalogUnsupported reports that a registry does not implement the
// standard Distribution catalog endpoint (/v2/_catalog). Callers receive this
// typed error instead of an empty list when the registry answers with HTTP
// 404 or the Distribution error code UNSUPPORTED.
var ErrCatalogUnsupported = errors.New("registry does not support the distribution catalog endpoint")

// defaultRepositoryListPageSize is the catalog page size the distribution
// cataloger requests. A registry may return fewer entries per page.
const defaultRepositoryListPageSize = 100

// DistributionCataloger lists repository names through the standard
// Distribution catalog endpoint (GET /v2/_catalog) with n and last pagination
// and Link header following. It is the only cataloger this package provides.
type DistributionCataloger struct {
	registry *remote.Registry
	logger   *slog.Logger
}

var _ RepositoryCataloger = (*DistributionCataloger)(nil)

// NewDistributionCataloger builds a DistributionCataloger from the given
// options.
func NewDistributionCataloger(opts CatalogerOptions) (RepositoryCataloger, error) {
	reg, err := remote.NewRegistry(opts.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client for host %q: %w", opts.Host, err)
	}
	reg.PlainHTTP = opts.PlainHTTP
	reg.RepositoryListPageSize = defaultRepositoryListPageSize
	// Pick the client in this order: an explicit auth client (so the token flow
	// and credentials apply), then the shared OCM HTTP client (so its TLS, proxy
	// and timeout settings apply). A provided auth client is used as is and is
	// not modified; the caller sets its base HTTP client (oci/credentials.AuthClient
	// already does). Without either, ORAS uses its default client.
	switch {
	case opts.AuthClient != nil:
		reg.Client = opts.AuthClient
	case opts.HTTPClient != nil:
		reg.Client = &auth.Client{Client: opts.HTTPClient, Cache: auth.NewCache()}
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "distribution-cataloger"), slog.String("host", opts.Host))

	return &DistributionCataloger{
		registry: reg,
		logger:   logger,
	}, nil
}

// Repositories lists repository names through the Distribution catalog
// endpoint. It delegates to ORAS remote.Registry.Repositories and maps a
// catalog-unsupported outcome (HTTP 404 or Distribution error code
// UNSUPPORTED) to ErrCatalogUnsupported. Errors returned by fn are passed
// through unchanged, so a callback failure is never reported as a catalog
// problem.
func (c *DistributionCataloger) Repositories(ctx context.Context, last string, fn func(repos []string) error) error {
	c.logger.DebugContext(ctx, "listing repositories via distribution catalog", "last", last)

	var callbackErr error
	err := c.registry.Repositories(ctx, last, func(repos []string) error {
		if e := fn(repos); e != nil {
			callbackErr = e
			return e
		}
		return nil
	})
	// A callback error is the caller's own error, not a registry error, so
	// return it unwrapped and do not classify it as catalog-unsupported.
	if callbackErr != nil {
		return callbackErr
	}
	if err == nil {
		return nil
	}
	if isCatalogUnsupported(err) {
		return fmt.Errorf("%w: %w", ErrCatalogUnsupported, err)
	}
	return fmt.Errorf("failed to list repositories: %w", err)
}

// isCatalogUnsupported reports whether err indicates that the registry does
// not implement the catalog endpoint. It matches an HTTP 404 response and the
// Distribution error code UNSUPPORTED.
func isCatalogUnsupported(err error) bool {
	var errResp *errcode.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.StatusCode == http.StatusNotFound {
			return true
		}
		for _, e := range errResp.Errors {
			if e.Code == errcode.ErrorCodeUnsupported {
				return true
			}
		}
	}

	var errCode errcode.Error
	if errors.As(err, &errCode) && errCode.Code == errcode.ErrorCodeUnsupported {
		return true
	}

	return false
}
