package internal

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"

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
      type: OCIRepository
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
