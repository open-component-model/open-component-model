package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_AddComponentVersion_OCIRepository(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting OCI repository add component-version integration test")

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
      type: OCIRegistry
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
      type: OCIRegistry
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
      type: OCIRegistry
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
			registry, err := internal.CreateOCIRegistry(t)
			r.NoError(err)

			cfg := fmt.Sprintf(tc.cfg, registry.Host, registry.Port, registry.User, registry.Password)
			cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
			r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

			t.Logf("Generated config:\n%s", cfg)

			client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)

			resolver, err := urlresolver.New(
				urlresolver.WithBaseURL(registry.RegistryAddress),
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
					"--repository", fmt.Sprintf("http://%s", registry.RegistryAddress),
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
					"--repository", fmt.Sprintf("oci::http://%s", registry.RegistryAddress),
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
					"--repository", fmt.Sprintf("http://%s", registry.RegistryAddress),
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

func Test_Integration_HelmInput_LocalPath(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	root := getRepoRootBasedOnGit(t)
	chartPath := filepath.Join(root, "bindings/go/helm/testdata/mychart")

	componentName := "ocm.software/helm-input-local"
	componentVersion := "0.1.0"

	constructor := fmt.Sprintf(`components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: mychart
    version: 0.1.0
    type: helmChart
    input:
      type: helm/v1
      path: %s
`, componentName, componentVersion, chartPath)

	tempDir := t.TempDir()
	constructorPath := filepath.Join(tempDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	ctfDir := filepath.Join(tempDir, "ctf")

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfDir),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "add cv with local helm input should succeed")

	// Download the resource from the CTF to verify it was stored correctly
	downloadDir := filepath.Join(tempDir, "downloaded")
	componentRef := fmt.Sprintf("ctf::%s//%s:%s", ctfDir, componentName, componentVersion)
	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"resource",
		componentRef,
		"--identity", "name=mychart,version=0.1.0",
		"--output", downloadDir,
	})
	r.NoError(downloadCMD.ExecuteContext(ctx), "download resource from CTF should succeed")

	layout := internal.ParseHelmOCILayout(t, downloadDir)
	layout.AssertHelmChartLayer(t)
}

func Test_Integration_HelmAccess_DigestProcessor(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// Serve the test chart and an index.yaml from a local HTTP server.
	// The digest is the SHA-256 of mychart-0.1.0.tgz as recorded in the index.
	root := getRepoRootBasedOnGit(t)
	chartDir := filepath.Join(root, "bindings/go/helm/testdata")
	chartDigest := "c68fb36429431f1bf40e539e52d93e49d41b7ab9a6eaceba43e103ca7043bfcb"

	indexYAML := fmt.Sprintf(`apiVersion: v1
entries:
  mychart:
    - urls:
        - mychart-0.1.0.tgz
      name: mychart
      version: 0.1.0
      digest: "sha256:%s"
      apiVersion: v2
generated: "2024-01-01T00:00:00.000000000Z"
`, chartDigest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/index.yaml":
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(indexYAML))
		default:
			http.FileServer(http.Dir(chartDir)).ServeHTTP(w, req)
		}
	}))
	t.Cleanup(srv.Close)

	componentName := "ocm.software/helm-access-digest"
	componentVersion := "0.1.0"

	constructorContent := fmt.Sprintf(`components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: mychart
    version: 0.1.0
    type: helmChart
    access:
      type: helm/v1
      helmRepository: %s
      helmChart: mychart:0.1.0
`, componentName, componentVersion, srv.URL)

	tempDir := t.TempDir()
	constructorPath := filepath.Join(tempDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	ctfDir := filepath.Join(tempDir, "ctf")

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfDir),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "add cv with helm access should succeed and trigger digest processor")

	// Open the CTF and verify the resource has a digest set by the Helm digest processor.
	fs, err := filesystem.NewFS(ctfDir, os.O_RDONLY)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)

	resource := desc.Component.Resources[0]
	r.Equal("mychart", resource.Name)
	r.NotNil(resource.Digest, "helm digest processor should have set a digest on the resource")
	r.Equal("SHA-256", resource.Digest.HashAlgorithm)
	r.Equal(chartDigest, resource.Digest.Value)
}

func Test_Integration_HelmInput_RemoteRepository(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// Serve the test chart from a local HTTP server
	root := getRepoRootBasedOnGit(t)
	chartDir := filepath.Join(root, "bindings/go/helm/testdata/provenance")
	srv := httptest.NewServer(http.FileServer(http.Dir(chartDir)))
	t.Cleanup(srv.Close)

	componentName := "ocm.software/helm-input-remote"
	componentVersion := "0.1.0"

	constructor := fmt.Sprintf(`components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: mychart
    version: 0.1.0
    type: helmChart
    input:
      type: helm/v1
      helmRepository: %s/mychart-0.1.0.tgz
`, componentName, componentVersion, srv.URL)

	tempDir := t.TempDir()
	constructorPath := filepath.Join(tempDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	ctfDir := filepath.Join(tempDir, "ctf")

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfDir),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "add cv with remote helm input should succeed")

	// Download the resource from the CTF to verify it was stored correctly
	downloadDir := filepath.Join(tempDir, "downloaded")
	componentRef := fmt.Sprintf("ctf::%s//%s:%s", ctfDir, componentName, componentVersion)
	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"resource",
		componentRef,
		"--identity", "name=mychart,version=0.1.0",
		"--output", downloadDir,
	})
	r.NoError(downloadCMD.ExecuteContext(ctx), "download resource from CTF should succeed")

	layout := internal.ParseHelmOCILayout(t, downloadDir)
	layout.AssertHelmChartLayer(t)
}

// Test_Integration_AddComponentVersion_HelmAccess verifies that a component version
// with a helm access resource (remote chart reference) can be added to an OCI registry.
// This exercises the Helm ResourceRepository plugin for resolving helm access types.
func Test_Integration_AddComponentVersion_HelmAccess(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	root := getRepoRootBasedOnGit(t)
	chartDir := filepath.Join(root, "bindings/go/helm/testdata/provenance")
	_, err := os.Stat(filepath.Join(chartDir, "mychart-0.1.0.tgz"))
	r.NoError(err, "test helm chart should exist")

	srv := httptest.NewServer(http.FileServer(http.Dir(chartDir)))
	t.Cleanup(srv.Close)

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

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	componentName := "ocm.software/helm-access-add-cv"
	componentVersion := "v1.0.0"

	constructor := fmt.Sprintf(`components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: mychart
    version: 0.1.0
    type: helmChart
    access:
      type: helm/v1
      helmRepository: %s
      helmChart: mychart-0.1.0.tgz
`, componentName, componentVersion, srv.URL)

	constructorPath := filepath.Join(tempDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("http://%s", registry.RegistryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
		"--skip-reference-digest-processing",
	})

	addCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(addCtx), "add cv with helm access to OCI registry should succeed")

	// Verify the component version was added by retrieving it
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve the added component version")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("mychart", desc.Component.Resources[0].Name)
	r.Equal("0.1.0", desc.Component.Resources[0].Version)
	r.Equal("helmChart", desc.Component.Resources[0].Type)
	r.Equal("helm/v1", desc.Component.Resources[0].Access.GetType().String())
}
