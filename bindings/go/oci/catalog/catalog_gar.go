package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// garScope is the OAuth2 scope the cataloger requests for a Google access
// token. The cloud-platform scope covers Artifact Registry and legacy GCR
// read access.
const garScope = "https://www.googleapis.com/auth/cloud-platform"

// garDefaultTokenSource returns a Google token source from Application Default
// Credentials. It is a package-private var so a unit test can inject a static
// token source and never reach Google.
var garDefaultTokenSource = func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
	return google.DefaultTokenSource(ctx, scopes...)
}

func init() {
	RegisterCataloger(garFactory{})
}

// garFactory builds a Google Artifact Registry or legacy GCR cataloger. Both
// backends implement the standard Distribution catalog endpoint, so the
// cataloger reuses the Distribution cataloger. The only Google specific part
// is the authentication with a short-lived OAuth2 access token.
type garFactory struct{}

var _ CatalogerFactory = garFactory{}

// Match reports whether host is an Artifact Registry host (<location>-docker.
// pkg.dev) or a legacy GCR host (gcr.io or <prefix>.gcr.io). The match is
// case-insensitive.
func (garFactory) Match(host string) bool {
	host = strings.ToLower(host)
	if host == "gcr.io" || strings.HasSuffix(host, ".gcr.io") {
		return true
	}
	return strings.HasSuffix(host, "-docker.pkg.dev")
}

// New builds a Google cataloger. When explicit credentials are present, the
// standard Distribution path with the provided auth client already
// authenticates, so New reuses it. Otherwise the cataloger authenticates with
// a Google access token from Application Default Credentials.
func (garFactory) New(opts CatalogerOptions) (RepositoryCataloger, error) {
	if hasGoogleCredentials(opts.Credentials) {
		return NewDistributionCataloger(opts)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "gar-cataloger"), slog.String("host", opts.Host))

	return &garCataloger{
		opts:           opts,
		logger:         logger,
		newTokenSource: garDefaultTokenSource,
	}, nil
}

// garCataloger lists repository names of an Artifact Registry or legacy GCR
// host through the Distribution catalog endpoint. It injects a Google OAuth2
// access token as a Bearer token on every request.
type garCataloger struct {
	opts           CatalogerOptions
	logger         *slog.Logger
	newTokenSource func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error)
}

var _ RepositoryCataloger = (*garCataloger)(nil)

// Repositories lists the raw repository names of the registry. It obtains a
// Google access token, builds an HTTP client that adds the token as a Bearer
// header, and delegates to the Distribution cataloger. The generic
// ComponentLister applies the component-descriptors/ filter and the subPath
// strip.
func (c *garCataloger) Repositories(ctx context.Context, last string, fn func(repos []string) error) error {
	c.logger.DebugContext(ctx, "obtaining google access token for catalog listing")

	tokenSource, err := c.newTokenSource(ctx, garScope)
	if err != nil {
		return fmt.Errorf("failed to obtain google credentials for host %q: %w", c.opts.Host, err)
	}

	// oauth2.NewClient returns an HTTP client whose transport adds the access
	// token as a Bearer header and refreshes it when it expires. When an OCM
	// HTTP client is present, it is the base transport so TLS and proxy
	// settings are honoured.
	tokenCtx := ctx
	if c.opts.HTTPClient != nil {
		tokenCtx = context.WithValue(ctx, oauth2.HTTPClient, c.opts.HTTPClient)
	}
	oauthClient := oauth2.NewClient(tokenCtx, tokenSource)

	// The access token is already on the request through the oauth2 transport,
	// so the auth client needs no credential function. It passes the request
	// through and only runs its own flow on an authentication challenge.
	opts := c.opts
	opts.AuthClient = &auth.Client{
		Client: oauthClient,
		Cache:  auth.NewCache(),
	}

	delegate, err := NewDistributionCataloger(opts)
	if err != nil {
		return err
	}
	return delegate.Repositories(ctx, last, fn)
}

// hasGoogleCredentials reports whether the credentials carry any field that
// authenticates against the registry. When they do, the standard Distribution
// path is used instead of Application Default Credentials.
func hasGoogleCredentials(creds *credsv1.OCICredentials) bool {
	if creds == nil {
		return false
	}
	return creds.Username != "" || creds.Password != "" || creds.AccessToken != "" || creds.RefreshToken != ""
}
