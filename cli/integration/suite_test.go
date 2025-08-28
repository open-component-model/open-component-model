package integration

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/integration/internal"
)

// TestSuite holds shared test infrastructure
type TestSuite struct {
	RegistryAddress string
	Username        string
	Password        string
	ConfigPath      string
	Repository      *oci.Repository
	once            sync.Once
}

var testSuite TestSuite

// SetupTestSuite initializes the shared test infrastructure once
func SetupTestSuite(t *testing.T) *TestSuite {
	testSuite.once.Do(func() {
		setupRegistryWithoutCleanup(t)
	})
	return &testSuite
}

// setupRegistryWithoutCleanup creates a containerized registry for OCI tests but doesn't tie cleanup to individual tests
func setupRegistryWithoutCleanup(t *testing.T) {
	r := require.New(t)

	t.Logf("Setting up shared test registry (no individual cleanup)")
	testSuite.Username = "ocm"

	testSuite.Password = internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, testSuite.Username, testSuite.Password)
	testSuite.RegistryAddress = startRegistryForSharing(t, htpasswd)
	host, port, err := net.SplitHostPort(testSuite.RegistryAddress)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository/v1
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`, host, port, testSuite.Username, testSuite.Password)

	globalTempDir := os.TempDir()
	testSuite.ConfigPath = filepath.Join(globalTempDir, fmt.Sprintf("ocmconfig-shared-%d.yaml", os.Getpid()))
	r.NoError(os.WriteFile(testSuite.ConfigPath, []byte(cfg), os.ModePerm))

	t.Logf("Generated shared config at: %s", testSuite.ConfigPath)

	client := internal.CreateAuthClient(testSuite.RegistryAddress, testSuite.Username, testSuite.Password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(testSuite.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	testSuite.Repository, err = oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	t.Logf("Shared test registry setup complete: %s", testSuite.RegistryAddress)
}

// CreateComponentConstructor creates a component constructor file for testing
func (ts *TestSuite) CreateComponentConstructor(t *testing.T, componentName, componentVersion string) string {
	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-resource
    version: %s
    type: plainText
    input:
      type: utf8
      text: "Hello, World from %s!"
`, componentName, componentVersion, componentVersion, componentName)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	require.NoError(t, os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))
	return constructorPath
}

// GetRepositoryURL returns the base repository URL for the shared registry
func (ts *TestSuite) GetRepositoryURL() string {
	return fmt.Sprintf("http://%s", ts.RegistryAddress)
}

// GetRepositoryURLWithPrefix returns the repository URL with the specified type prefix
func (ts *TestSuite) GetRepositoryURLWithPrefix(prefix string) string {
	return fmt.Sprintf("%s::%s", prefix, ts.GetRepositoryURL())
}

// startRegistryForSharing starts a registry container that will be cleaned up by testcontainers' ryuk, not individual tests
func startRegistryForSharing(t *testing.T, htpasswd string) string {
	const distributionRegistryImage = "registry:3.0.0"

	t.Helper()
	r := require.New(t)

	t.Logf("Launching shared test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(t.Context(), distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)

	// NOTE: Intentionally NOT calling t.Cleanup() here
	// The container will be cleaned up by testcontainer when the process ends
	// This prevents individual tests from terminating the shared registry

	t.Logf("Shared test registry started")

	registryAddress, err := registryContainer.HostAddress(t.Context())
	r.NoError(err)

	return registryAddress
}
