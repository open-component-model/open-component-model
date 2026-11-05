// Package registrytest starts OCI registries for integration tests.
//
// [Start] needs Docker, terminates the registry when the test ends, and returns its
// host:port. Callers add the scheme they need.
//
//	reg := registrytest.Start(t)
//	authed := registrytest.Start(t, registrytest.WithAuth("user", "pass"))
//	client := registrytest.AuthClient(authed.Address, authed.Username, authed.Password)
//
// [Htpasswd] returns one htpasswd line for a registry the test configures itself, and
// [AuthClient] an ORAS client that logs in to any registry, Nexus included.
package registrytest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// Image is the OCI distribution registry [Start] runs.
	Image = "registry:3.0.0"

	startTimeout = 60 * time.Second
	userAgent    = "ocm.software/integration-test"
)

// Registry is a running OCI registry. Username and Password are empty when the registry
// accepts anonymous access.
type Registry struct {
	Address  string
	Username string
	Password string
}

// Option configures [Start].
type Option func(*Registry)

// WithAuth makes the registry require basic auth with the given username and password.
func WithAuth(username, password string) Option {
	return func(reg *Registry) {
		reg.Username = username
		reg.Password = password
	}
}

// Start starts an OCI distribution registry that is reachable over plain HTTP. Without
// [WithAuth] it accepts anonymous access.
func Start(t *testing.T, opts ...Option) Registry {
	t.Helper()
	r := require.New(t)

	var reg Registry
	for _, opt := range opts {
		opt(&reg)
	}

	runOpts := []testcontainers.ContainerCustomizer{
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
	}
	if reg.Username != "" {
		runOpts = append(runOpts, registry.WithHtpasswd(Htpasswd(t, reg.Username, reg.Password)))
	}

	ctx, cancel := context.WithTimeout(t.Context(), startTimeout)
	defer cancel()
	container, err := registry.Run(ctx, Image, runOpts...)
	testcontainers.CleanupContainer(t, container)
	r.NoError(err, "should start registry container")

	reg.Address, err = container.HostAddress(ctx)
	r.NoError(err)
	return reg
}

// Htpasswd returns an htpasswd line for username with a bcrypt hash of password.
func Htpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, hash)
}

// AuthClient returns an ORAS client that sends username and password as basic auth to the
// registry at address, which may be a host:port or a URL. It panics on an invalid address
// so it can be used in expressions that have no *testing.T at hand.
func AuthClient(address, username, password string) *auth.Client {
	url, err := runtime.ParseURLAndAllowNoScheme(address)
	if err != nil {
		panic(fmt.Sprintf("invalid address %q: %v", address, err))
	}
	return &auth.Client{
		Client:     retry.DefaultClient,
		Header:     http.Header{"User-Agent": []string{userAgent}},
		Credential: auth.StaticCredential(url.Host, auth.Credential{Username: username, Password: password}),
	}
}
