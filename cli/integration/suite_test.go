package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
}

var (
	globalTestSuite *TestSuite
)

// TestMain sets up and tears down the test suite
func TestMain(m *testing.M) {
	var exitCode int

	// Setup
	globalTestSuite = &TestSuite{}
	err := globalTestSuite.setup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup test suite: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	exitCode = m.Run()

	// Teardown
	globalTestSuite.teardown()

	os.Exit(exitCode)
}

// SetupTestSuite returns the global test suite instance
func SetupTestSuite(t *testing.T) *TestSuite {
	if globalTestSuite == nil {
		t.Fatal("Test suite not initialized. TestMain should handle this.")
	}
	return globalTestSuite
}

// setup initializes the test suite
func (ts *TestSuite) setup() error {
	ctx := context.Background()

	ts.Username = "ocm"

	// Generate password and credentials
	ts.Password = internal.GenerateRandomPassword(&testing.T{}, 20)
	htpasswd := internal.GenerateHtpasswd(&testing.T{}, ts.Username, ts.Password)

	// Start registry container
	registryContainer, err := registry.Run(ctx, "registry:3.0.0",
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
		testcontainers.WithLogger(log.Default()),
	)
	if err != nil {
		return fmt.Errorf("failed to start registry: %w", err)
	}

	ts.RegistryAddress, err = registryContainer.HostAddress(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registry address: %w", err)
	}

	host, port, err := net.SplitHostPort(ts.RegistryAddress)
	if err != nil {
		return fmt.Errorf("failed to parse registry address: %w", err)
	}

	// Generate OCM config
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
`, host, port, ts.Username, ts.Password)

	globalTempDir := os.TempDir()
	ts.ConfigPath = filepath.Join(globalTempDir, fmt.Sprintf("ocmconfig-suite-%d.yaml", os.Getpid()))
	if err := os.WriteFile(ts.ConfigPath, []byte(cfg), os.ModePerm); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Setup OCI client and repository
	client := internal.CreateAuthClient(ts.RegistryAddress, ts.Username, ts.Password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(ts.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	if err != nil {
		return fmt.Errorf("failed to create resolver: %w", err)
	}

	ts.Repository, err = oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(os.TempDir()))
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	fmt.Printf("Shared test registry setup complete: %s\n", ts.RegistryAddress)
	return nil
}

// teardown cleans up the test suite
func (ts *TestSuite) teardown() {
	if ts.ConfigPath != "" {
		if err := os.Remove(ts.ConfigPath); err != nil {
			fmt.Printf("Warning: failed to cleanup config file %s: %v\n", ts.ConfigPath, err)
		}
	}
	fmt.Println("Suite teardown complete")
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
