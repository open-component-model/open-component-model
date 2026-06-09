package internal

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}<>?"

func GenerateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, hashedPassword)
}

func GenerateRandomPassword(t *testing.T, length int) string {
	t.Helper()
	password := make([]byte, length)
	for i := range password {
		randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		require.NoError(t, err)
		password[i] = charset[randomIndex.Int64()]
	}
	return string(password)
}

func CreateAuthClient(address, username, password string) *auth.Client {
	url, err := ocmruntime.ParseURLAndAllowNoScheme(address)
	if err != nil {
		panic(fmt.Sprintf("invalid address %q: %v", address, err))
	}
	return &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{
			"User-Agent": []string{"ocm.software/integration-test"},
		},
		Credential: auth.StaticCredential(url.Host, auth.Credential{
			Username: username,
			Password: password,
		}),
	}
}

const distributionRegistryImage = "registry:3.0.0"

func StartDockerContainerRegistry(t *testing.T, container, htpasswd string) string {
	t.Helper()
	// Make sure the registry starts clean: drop any container that still carries
	// this name from a previously crashed or interrupted run, so testcontainers'
	// WithName can never collide with — or reuse the stale state of — a leftover.
	removeContainerByName(t, t.Context(), container)
	// Start containerized registry
	t.Logf("Launching test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(t.Context(), distributionRegistryImage,
		WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
		testcontainers.WithName(container),
	)
	r := require.New(t)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})
	t.Logf("Test registry started")

	registryAddress, err := registryContainer.HostAddress(t.Context())
	r.NoError(err)

	return registryAddress
}

// removeContainerByName force-removes any container that already carries name —
// e.g. one left behind by a previously crashed or interrupted test run — so a
// freshly started container can never reuse or collide with stale state. It goes
// through testcontainers' Docker client so it targets the same daemon
// testcontainers does. A missing container is the normal, expected case and is
// only logged, never fatal.
func removeContainerByName(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		t.Logf("clean-start: could not open docker client to remove leftover container %q: %v", name, err)
		return
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.ContainerRemove(ctx, name, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		// Almost always "No such container" — the happy path of a clean machine.
		t.Logf("clean-start: no leftover container %q to remove: %v", name, err)
		return
	}
	t.Logf("clean-start: removed leftover container %q before launching a fresh one", name)
}

func WithHtpasswd(credentials string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		tmpFile, err := os.CreateTemp("", "htpasswd")
		if err != nil {
			tmpFile, err = os.Create(".")
			if err != nil {
				return fmt.Errorf("cannot create the file in the temp dir or in the current dir: %w", err)
			}
		}
		defer tmpFile.Close()

		_, err = tmpFile.WriteString(credentials)
		if err != nil {
			return fmt.Errorf("cannot write the credentials to the file: %w", err)
		}

		return registry.WithHtpasswdFile(tmpFile.Name())(req)
	}
}

type ConfigOpts struct {
	Host, Port, User, Password string
}

func CreateOCMConfigForRegistry(t *testing.T, opts []ConfigOpts) (string, error) {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	cfg := `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:`

	for _, o := range opts {
		cfg += fmt.Sprintf(`
  - identity:
      type: OCIRegistry
      hostname: %q
      port: %q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q`, o.Host, o.Port, o.User, o.Password)
	}

	if err := os.WriteFile(cfgPath, []byte(cfg), os.ModePerm); err != nil { //nolint:gosec // test code
		return "", err
	}

	t.Logf("Generated config:%s", cfg)

	return cfgPath, nil
}
