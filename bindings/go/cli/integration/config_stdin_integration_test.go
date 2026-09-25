package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
)

// Test_Integration_ConfigFromStdin proves that a configuration piped through `--config -`
// reaches the credential graph. The control run without valid stdin credentials shows
// that a success cannot come from configuration found elsewhere.
func Test_Integration_ConfigFromStdin(t *testing.T) {
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")
	repo := fmt.Sprintf("http://%s", registry.RegistryAddress)

	t.Run("registry credentials", func(t *testing.T) {
		constructor := writeMinimalConstructor(t, "ocm.software/config-stdin-test", "v1.0.0")

		t.Run("wrong password on stdin fails", func(t *testing.T) {
			err := runOCM(t, registryConfig(registry, registry.Password+"-invalid"),
				"add", "component-version", "--repository", repo, "--constructor", constructor, "--config", "-")
			require.ErrorContains(t, err, "401")
		})

		t.Run("credentials on stdin authenticate", func(t *testing.T) {
			r := require.New(t)
			err := runOCM(t, registryConfig(registry, registry.Password),
				"add", "component-version", "--repository", repo, "--constructor", constructor, "--config", "-")
			r.NoError(err)

			desc, err := registry.Connect(t).GetComponentVersion(t.Context(), "ocm.software/config-stdin-test", "v1.0.0")
			r.NoError(err, "component version should exist in the registry")
			r.Equal("ocm.software/config-stdin-test", desc.Component.Name)
		})
	})
}

// runOCM runs one CLI command with the given stdin and a bounded context.
func runOCM(t *testing.T, stdin []byte, args ...string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	c := cmd.New()
	c.SetIn(bytes.NewReader(stdin))
	c.SetArgs(args)
	return c.ExecuteContext(ctx)
}

// registryConfig renders the credentials for the registry in memory, so the secret
// under test never touches disk.
func registryConfig(registry *internal.OCIRegistry, password string) []byte {
	return fmt.Appendf(nil, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %q
      port: %q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q
`, registry.Host, registry.Port, registry.User, password)
}

func writeMinimalConstructor(t *testing.T, name, version string) string {
	t.Helper()
	content := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
`, name, version)
	path := filepath.Join(t.TempDir(), "constructor.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), os.ModePerm))
	return path
}
