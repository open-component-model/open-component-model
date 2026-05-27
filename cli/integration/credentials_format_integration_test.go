package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// constructorYAML returns a minimal component constructor for use in add component-version tests.
func constructorYAML(componentName, componentVersion string) string {
	return fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "hello"
`, componentName, componentVersion)
}

// Test_Credentials_OldFormat verifies that the legacy Credentials/v1 properties-map format
// still allows the CLI to push to an authenticated OCI registry.
func Test_Credentials_OldFormat(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`, registry.Host, registry.Port, registry.User, registry.Password)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	constructorPath := filepath.Join(dir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML("ocm.software/test", "v1.0.0")), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", registry.RegistryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(ctx), "add component-version with Credentials/v1 must succeed")
}

// Test_Credentials_NewFormat verifies that the new OCICredentials/v1 typed format
// allows the CLI to push to an authenticated OCI registry when CredentialTypeSchemeProvider
// is wired (which it now is via builtin.Register → CredentialTypeRegistry).
func Test_Credentials_NewFormat(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: OCICredentials/v1
      username: %[3]q
      password: %[4]q
`, registry.Host, registry.Port, registry.User, registry.Password)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	constructorPath := filepath.Join(dir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML("ocm.software/test", "v1.0.0")), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", registry.RegistryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(ctx), "add component-version with OCICredentials/v1 must succeed")
}
