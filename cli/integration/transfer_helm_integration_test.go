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

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	helmv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/integration/internal"
)

// newHelmChartRepoServer starts an httptest server that serves files from the given directory.
func newHelmChartRepoServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv
}

func Test_Integration_Transfer_HelmChart(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	t.Parallel()

	// 1. Locate the test helm chart and start an HTTP server to host it
	root := getRepoRootBasedOnGit(t)
	chartDir := filepath.Join(root, "bindings/go/helm/testdata/provenance")
	_, err := os.Stat(filepath.Join(chartDir, "mychart-0.1.0.tgz"))
	r.NoError(err, "test helm chart should exist")

	srv := newHelmChartRepoServer(t, chartDir)
	t.Logf("Helm chart repo server at %s", srv.URL)

	// 2. Setup target OCI registry
	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start target registry container")

	cfg := fmt.Sprintf(`
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
`, targetRegistry.Host, targetRegistry.Port, targetRegistry.User, targetRegistry.Password)

	tempdir := t.TempDir()
	cfgPath := filepath.Join(tempdir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// Set up repository provider and credential resolver to verify transfers
	repoProvider := provider.NewComponentVersionRepositoryProvider()
	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	r.NoError(err)
	credconf, err := runtime.LookupCredentialConfig(ocmconf)
	r.NoError(err)
	credentialResolver, err := credentials.ToGraph(ctx, credconf, credentials.Options{
		RepositoryPluginProvider: credentials.GetRepositoryPluginFn(func(ctx context.Context, typed ocmruntime.Typed) (credentials.RepositoryPlugin, error) {
			return nil, fmt.Errorf("no repository plugin configured for type %s", typed.GetType().String())
		}),
		CredentialPluginProvider: credentials.GetCredentialPluginFn(func(ctx context.Context, typed ocmruntime.Typed) (credentials.CredentialPlugin, error) {
			return nil, fmt.Errorf("no credential plugin configured for type %s", typed.GetType().String())
		}),
		CredentialRepositoryTypeScheme: ocmruntime.NewScheme(),
	})
	r.NoError(err)

	// 3. Create a constructor that references the helm chart via access
	componentName := "ocm.software/test-helm-component"
	componentVersion := "v1.0.0"

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
      helmChart: mychart-0.1.0.tgz
`, componentName, componentVersion, srv.URL)

	constructorPath := filepath.Join(tempdir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(tempdir, "source-ctf")

	// 4. Create source CTF
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--skip-reference-digest-processing",
	})
	r.NoError(addCMD.ExecuteContext(ctx), "creation of component version should succeed")

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)

	t.Run("transfer helm chart to OCI registry", func(t *testing.T) {
		r := require.New(t)
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "helm-transfer")

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			sourceRef,
			targetRef,
			"--config", cfgPath,
			"--copy-resources",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

		// Check if component exists in target registry
		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err, "should be able to create target repository")

		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal("mychart", desc.Component.Resources[0].Name)

		// The helm chart should have been converted to an OCI artifact during transfer
		var helmAccess helmv1.OCIImage
		r.NoError(helmaccess.Scheme.Convert(desc.Component.Resources[0].Access, &helmAccess),
			"resource access should be convertible to OCIImage")
		r.NotEmpty(helmAccess.ImageReference, "image reference should not be empty")
	})

	t.Run("transfer helm chart with --upload-as ociArtifact", func(t *testing.T) {
		r := require.New(t)
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "helm-transfer-oci")

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			sourceRef,
			targetRef,
			"--config", cfgPath,
			"--copy-resources",
			"--upload-as", "ociArtifact",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err, "should be able to create target repository")

		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal("mychart", desc.Component.Resources[0].Name)

		var ociAccess helmv1.OCIImage
		r.NoError(ociaccess.Scheme.Convert(desc.Component.Resources[0].Access, &ociAccess),
			"resource access should be an OCI artifact")
		r.Contains(ociAccess.ImageReference, targetRef,
			"image reference should point to the target registry")
	})

	t.Run("transfer helm chart with local input to OCI registry", func(t *testing.T) {
		r := require.New(t)

		// Create a separate CTF using helm input (local path) instead of access
		chartPath := filepath.Join(root, "bindings/go/helm/testdata/provenance/mychart-0.1.0.tgz")
		localConstructor := fmt.Sprintf(`components:
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

		localTempDir := t.TempDir()
		localConstructorPath := filepath.Join(localTempDir, "constructor.yaml")
		r.NoError(os.WriteFile(localConstructorPath, []byte(localConstructor), os.ModePerm))

		localCTF := filepath.Join(localTempDir, "source-ctf")
		localAddCMD := cmd.New()
		localAddCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", fmt.Sprintf("ctf::%s", localCTF),
			"--constructor", localConstructorPath,
		})
		r.NoError(localAddCMD.ExecuteContext(ctx), "creation of component version with local helm input should succeed")

		localSourceRef := fmt.Sprintf("ctf::%s//%s:%s", localCTF, componentName, componentVersion)
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "helm-local-transfer")

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			localSourceRef,
			targetRef,
			"--config", cfgPath,
			"--copy-resources",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err, "should be able to create target repository")

		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal("mychart", desc.Component.Resources[0].Name)

		// Verify the resource has a local blob access (default transfer behavior)
		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[0].Access, &localBlobAccess),
			"resource should have local blob access after transfer")
	})
}
