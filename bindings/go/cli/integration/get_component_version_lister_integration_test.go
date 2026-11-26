package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
)

// listerCredentialConfig authenticates against the test registry over plain HTTP.
const listerCredentialConfig = `
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
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`

// listerPlainHTTPOnlyConfig has no credentials, only plain HTTP.
const listerPlainHTTPOnlyConfig = `
type: generic.config.ocm.software/v1
configurations:
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`

// Test_Integration_GetComponentVersion_RepositoryReference lists components of a
// private registry and looks up their versions through the CLI. It is the
// regression test for the version lookup that previously ran without
// credentials: listing succeeded but the version lookup failed with 401.
func Test_Integration_GetComponentVersion_RepositoryReference(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(listerCredentialConfig, registry.Host, registry.Port, registry.User, registry.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	repoRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	// Push two components into the private registry.
	constructor := `
components:
- name: ocm.software/app-a
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: text
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "a"
- name: ocm.software/app-b
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: text
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "b"`
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", repoRef,
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed")

	t.Run("with credentials lists components and their versions", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		getCMD := cmd.New()
		getCMD.SetOut(out)
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--latest",
			"--output", "json",
			"--config", cfgPath,
		})
		r.NoError(getCMD.ExecuteContext(t.Context()), "get component-version for a repository reference should succeed")

		printed := out.String()
		r.Contains(printed, "ocm.software/app-a")
		r.Contains(printed, "ocm.software/app-b")
		r.Contains(printed, "v1.0.0")
	})

	t.Run("without credentials the command fails instead of returning nothing", func(t *testing.T) {
		r := require.New(t)
		noCredsPath := filepath.Join(t.TempDir(), "nocreds.yaml")
		r.NoError(os.WriteFile(noCredsPath, []byte(listerPlainHTTPOnlyConfig), os.ModePerm))

		getCMD := cmd.New()
		getCMD.SetOut(new(bytes.Buffer))
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--latest",
			"--config", noCredsPath,
		})
		r.Error(getCMD.ExecuteContext(t.Context()), "listing a private registry without credentials must fail")
	})
}
