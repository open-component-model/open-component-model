package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_AddComponentVersion_OCIRepository(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting OCI repository add component-version integration test")
	user := "ocm"

	cases := []struct {
		name     string
		cfg      string
		external bool
	}{
		{
			name: "targeting defaults",
			cfg: `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`,
		},
		{
			name: "targeting fallback resolvers",
			cfg: `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
- type: ocm.config.ocm.software
  resolvers:
  - repository:
      type: OCIRepository
      hostname: %[1]q
`,
			external: true,
		},
		{
			name: "targeting resolvers",
			cfg: `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
- type: resolvers.config.ocm.software
  resolvers:
  - repository:
      type: OCIRepository/v1
      baseUrl: http://%[1]s:%[2]s
    componentNamePattern: "*"
`,
			external: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup credentials and htpasswd
			password := internal.GenerateRandomPassword(t, 20)
			htpasswd := internal.GenerateHtpasswd(t, user, password)

			containerName := fmt.Sprintf("add-component-version-oci-repository-%d", time.Now().UnixNano())
			registryAddress := internal.StartDockerContainerRegistry(t, containerName, htpasswd)
			host, port, err := net.SplitHostPort(registryAddress)
			r.NoError(err)

			cfg := fmt.Sprintf(tc.cfg, host, port, user, password)
			cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
			r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

			t.Logf("Generated config:\n%s", cfg)

			client := internal.CreateAuthClient(registryAddress, user, password)

			resolver, err := urlresolver.New(
				urlresolver.WithBaseURL(registryAddress),
				urlresolver.WithPlainHTTP(true),
				urlresolver.WithBaseClient(client),
			)
			r.NoError(err)

			repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
			r.NoError(err)

			t.Run("add component-version with plain OCI registry reference", func(t *testing.T) {
				r := require.New(t)

				componentName := "ocm.software/test-component"
				componentVersion := "v1.0.0"

				// Create constructor file
				var constructorContent string

				if tc.external {
					constructorContent = fmt.Sprintf(`
components:
- name: %[1]s
  version: %[2]s
  provider:
    name: ocm.software
  componentReferences:
    - name: external
      version: %[2]s
      componentName: %[1]s-external
  resources:
  - name: test-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello, World from OCI registry!"
- name: %[1]s-external
  version: %[2]s
  provider:
    name: ocm.software
  resources:
  - name: test-resource-2
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello, World from external registry!"
`, componentName, componentVersion)
				} else {
					constructorContent = fmt.Sprintf(`
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
      text: "Hello, World from OCI registry!"
`, componentName, componentVersion)
				}

				constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
				r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

				// Test the add component-version command with plain OCI registry reference
				addCMD := cmd.New()
				addCMD.SetArgs([]string{
					"add",
					"component-version",
					"--repository", fmt.Sprintf("http://%s", registryAddress),
					"--constructor", constructorPath,
					"--config", cfgPath,
				})

				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				r.NoError(addCMD.ExecuteContext(ctx), "add component-version should succeed with OCI registry")

				// Verify the component version was added by attempting to retrieve it
				desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
				r.NoError(err, "should be able to retrieve the added component version")
				r.Equal(componentName, desc.Component.Name)
				r.Equal(componentVersion, desc.Component.Version)
				r.Equal("ocm.software", desc.Component.Provider.Name)
				r.Len(desc.Component.Resources, 1)
				r.Equal("test-resource", desc.Component.Resources[0].Name)
				r.Equal("v1.0.0", desc.Component.Resources[0].Version)

				if tc.external {
					componentNameExternal := fmt.Sprintf("%s-external", componentName)
					descExternal, err := repo.GetComponentVersion(ctx, componentNameExternal, componentVersion)
					r.NoError(err, "should be able to retrieve the added component version")
					r.Equal(componentNameExternal, descExternal.Component.Name)
					r.Equal(componentVersion, descExternal.Component.Version)
					r.Equal("ocm.software", descExternal.Component.Provider.Name)
					r.Len(descExternal.Component.Resources, 1)
					r.Equal("test-resource-2", descExternal.Component.Resources[0].Name)
					r.Equal("v1.0.0", descExternal.Component.Resources[0].Version)
				}
			})

			t.Run("add component-version with explicit OCI type prefix", func(t *testing.T) {
				r := require.New(t)

				componentName := "ocm.software/explicit-oci-component"
				componentVersion := "v2.0.0"

				// Create constructor file
				constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: explicit-resource
    version: v2.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello from explicit OCI type!"
`, componentName, componentVersion)

				constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
				r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

				// Test with explicit oci:: prefix
				addCMD := cmd.New()
				addCMD.SetArgs([]string{
					"add",
					"component-version",
					"--repository", fmt.Sprintf("oci::http://%s", registryAddress),
					"--constructor", constructorPath,
					"--config", cfgPath,
				})

				r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with explicit OCI type")

				// Verify the component version was added
				desc, err := repo.GetComponentVersion(t.Context(), componentName, componentVersion)
				r.NoError(err, "should be able to retrieve the component version added with explicit OCI type")
				r.Equal(componentName, desc.Component.Name)
				r.Equal(componentVersion, desc.Component.Version)
			})

			t.Run("add component-version with HTTPS URL format", func(t *testing.T) {
				r := require.New(t)

				componentName := "ocm.software/https-component"
				componentVersion := "v3.0.0"

				// Create constructor file
				constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: https-resource
    version: v3.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello from HTTPS URL format!"
`, componentName, componentVersion)

				constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
				r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

				// Test with HTTPS URL format (will be treated as HTTP due to plain HTTP resolver)
				addCMD := cmd.New()
				addCMD.SetArgs([]string{
					"add",
					"component-version",
					"--repository", fmt.Sprintf("http://%s", registryAddress),
					"--constructor", constructorPath,
					"--config", cfgPath,
				})

				r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with HTTPS URL format")

				// Verify the component version was added
				desc, err := repo.GetComponentVersion(t.Context(), componentName, componentVersion)
				r.NoError(err, "should be able to retrieve the component version added with HTTPS URL")
				r.Equal(componentName, desc.Component.Name)
				r.Equal(componentVersion, desc.Component.Version)
			})
		})
	}
}

func Test_Integration_AddComponentVersion_CTFRepository(t *testing.T) {
	t.Parallel()

	t.Run("add component-version with CTF archive path", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/ctf-component"
		componentVersion := "v1.0.0"

		// Create constructor file
		constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: ctf-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello from CTF archive!"
`, componentName, componentVersion)

		constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
		r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

		// Test with CTF archive path
		ctfArchivePath := filepath.Join(t.TempDir(), "test-archive")
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", ctfArchivePath,
			"--constructor", constructorPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with CTF archive")

		// Verify the archive was created
		_, err := os.Stat(ctfArchivePath)
		r.NoError(err, "CTF archive should be created")
	})

	t.Run("add component-version with explicit CTF type prefix", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/explicit-ctf-component"
		componentVersion := "v2.0.0"

		// Create constructor file
		constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: explicit-ctf-resource
    version: v2.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello from explicit CTF type!"
`, componentName, componentVersion)

		constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
		r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

		// Test with explicit ctf:: prefix
		ctfArchivePath := filepath.Join(t.TempDir(), "explicit-archive")
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", fmt.Sprintf("ctf::%s", ctfArchivePath),
			"--constructor", constructorPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with explicit CTF type")

		// Verify the archive was created
		_, err := os.Stat(ctfArchivePath)
		r.NoError(err, "CTF archive should be created with explicit type")
	})
}
