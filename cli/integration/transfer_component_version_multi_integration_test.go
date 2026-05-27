package integration

import (
	"context"
	"fmt"
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

// buildMultiVersionSourceCTF creates a source CTF with multiple versions of a component
// using the `add component-version` command and returns the CTF directory path.
func buildMultiVersionSourceCTF(t *testing.T, componentName string, versions []string) string {
	t.Helper()
	r := require.New(t)
	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")

	for _, version := range versions {
		constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources: []
`, componentName, version)

		constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
		r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
			"--constructor", constructorPath,
		})
		r.NoError(addCMD.ExecuteContext(t.Context()), "creation of source CTF version %s should succeed", version)
	}

	return sourceCTF
}

// verifyVersionPresent checks that the given component version exists in the target OCI registry.
func verifyVersionPresent(t *testing.T, ctx context.Context, repo *oci.Repository, componentName, version string) {
	t.Helper()
	desc, err := repo.GetComponentVersion(ctx, componentName, version)
	require.NoError(t, err, "version %s should be present in target", version)
	require.Equal(t, componentName, desc.Component.Name)
	require.Equal(t, version, desc.Component.Version)
}

// verifyVersionAbsent checks that the given component version does NOT exist in the target OCI registry.
func verifyVersionAbsent(t *testing.T, ctx context.Context, repo *oci.Repository, componentName, version string) {
	t.Helper()
	_, err := repo.GetComponentVersion(ctx, componentName, version)
	require.Error(t, err, "version %s should NOT be present in target", version)
}

func Test_Integration_TransferComponentVersion_AllVersions(t *testing.T) {
	r := require.New(t)
	t.Parallel()

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
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	componentName := "ocm.software/test-multi-all"
	versions := []string{"v1.0.0", "v1.1.0", "v2.0.0"}
	sourceCTF := buildMultiVersionSourceCTF(t, componentName, versions)

	// Source ref without version → all versions discovered.
	sourceRef := fmt.Sprintf("ctf::%s//%s", sourceCTF, componentName)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx), "multi-version transfer should succeed")

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	for _, v := range versions {
		verifyVersionPresent(t, ctx, targetRepo, componentName, v)
	}
}

func Test_Integration_TransferComponentVersion_SemverConstraint(t *testing.T) {
	r := require.New(t)
	t.Parallel()

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
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	componentName := "ocm.software/test-multi-semver"
	sourceCTF := buildMultiVersionSourceCTF(t, componentName, []string{"1.0.0", "1.1.0", "2.0.0"})

	sourceRef := fmt.Sprintf("ctf::%s//%s", sourceCTF, componentName)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--semver-constraint", "< 2.0.0",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx), "semver-constrained transfer should succeed")

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	verifyVersionPresent(t, ctx, targetRepo, componentName, "1.0.0")
	verifyVersionPresent(t, ctx, targetRepo, componentName, "1.1.0")
	verifyVersionAbsent(t, ctx, targetRepo, componentName, "2.0.0")
}

func Test_Integration_TransferComponentVersion_LatestOnly(t *testing.T) {
	r := require.New(t)
	t.Parallel()

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
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	componentName := "ocm.software/test-multi-latest"
	sourceCTF := buildMultiVersionSourceCTF(t, componentName, []string{"1.0.0", "1.1.0", "2.0.0"})

	sourceRef := fmt.Sprintf("ctf::%s//%s", sourceCTF, componentName)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--latest",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx), "latest-only transfer should succeed")

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	verifyVersionPresent(t, ctx, targetRepo, componentName, "2.0.0")
	verifyVersionAbsent(t, ctx, targetRepo, componentName, "1.0.0")
	verifyVersionAbsent(t, ctx, targetRepo, componentName, "1.1.0")
}
