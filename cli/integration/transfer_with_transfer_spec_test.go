package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// createSourceCTF creates a CTF archive with a single plainText resource and returns the source reference string.
func createSourceCTF(t *testing.T, componentName, componentVersion string) string {
	t.Helper()
	r := require.New(t)

	constructorContent := fmt.Sprintf(`
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
      text: "Hello from transfer-spec integration test!"
`, componentName, componentVersion)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of source CTF should succeed")

	return fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
}

// generateTransferSpec runs a dry-run transfer and returns the YAML spec.
func generateTransferSpec(t *testing.T, sourceRef, targetRef, cfgPath string) []byte {
	t.Helper()
	r := require.New(t)

	dryRunCMD := cmd.New()
	specOutput := new(bytes.Buffer)
	dryRunCMD.SetOut(specOutput)
	dryRunCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--dry-run", "-o", "yaml",
	})
	r.NoError(dryRunCMD.ExecuteContext(t.Context()), "dry-run should succeed")
	r.NotEmpty(specOutput.Bytes(), "dry-run should produce output")

	return specOutput.Bytes()
}

// executeTransferSpec runs the transfer command with a spec file.
func executeTransferSpec(t *testing.T, specFile, cfgPath string) {
	t.Helper()

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		"--transfer-spec", specFile,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	require.NoError(t, transferCMD.ExecuteContext(ctx), "transfer from spec should succeed")
}

// connectToOCIRegistry creates a repository client for an OCI registry.
func connectToOCIRegistry(t *testing.T, reg *internal.OCIRegistry) *oci.Repository {
	t.Helper()
	r := require.New(t)

	client := internal.CreateAuthClient(reg.RegistryAddress, reg.User, reg.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(reg.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	return repo
}

// Test_Integration_TransferWithTransferSpec_CTFToOCI performs a two-step transfer:
// 1. Generate the transfer spec via --dry-run
// 2. Execute the spec via --transfer-spec
func Test_Integration_TransferWithTransferSpec_CTFToOCI(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	componentName := "ocm.software/transfer-spec-test"
	componentVersion := "v1.0.0"

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{{
		Host: registry.Host, Port: registry.Port,
		User: registry.User, Password: registry.Password,
	}})
	r.NoError(err)

	sourceRef := createSourceCTF(t, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	spec := generateTransferSpec(t, sourceRef, targetRef, cfgPath)

	specFile := filepath.Join(t.TempDir(), "transfer-spec.yaml")
	r.NoError(os.WriteFile(specFile, spec, os.ModePerm))

	executeTransferSpec(t, specFile, cfgPath)

	// Verify component exists in target registry
	repo := connectToOCIRegistry(t, registry)
	desc, err := repo.GetComponentVersion(t.Context(), componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)
}

// Test_Integration_TransferWithTransferSpec_ModifiedTarget generates a spec pointing
// to one OCI registry, edits it to point to a different registry, and verifies the
// component lands in the modified target only.
func Test_Integration_TransferWithTransferSpec_ModifiedTarget(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	componentName := "ocm.software/modified-target-spec-test"
	componentVersion := "v1.0.0"

	registryA, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry A")

	registryB, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry B")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: registryA.Host, Port: registryA.Port, User: registryA.User, Password: registryA.Password},
		{Host: registryB.Host, Port: registryB.Port, User: registryB.User, Password: registryB.Password},
	})
	r.NoError(err)

	sourceRef := createSourceCTF(t, componentName, componentVersion)
	targetRefA := fmt.Sprintf("http://%s", registryA.RegistryAddress)

	// Generate spec targeting registry A, then modify to target registry B
	spec := generateTransferSpec(t, sourceRef, targetRefA, cfgPath)
	modifiedSpec := strings.ReplaceAll(string(spec), registryA.RegistryAddress, registryB.RegistryAddress)
	r.NotEqual(string(spec), modifiedSpec, "spec should contain registry A address to replace")

	specFile := filepath.Join(t.TempDir(), "transfer-spec.yaml")
	r.NoError(os.WriteFile(specFile, []byte(modifiedSpec), os.ModePerm))

	executeTransferSpec(t, specFile, cfgPath)

	// Verify component exists in registry B (the modified target)
	repoB := connectToOCIRegistry(t, registryB)
	desc, err := repoB.GetComponentVersion(t.Context(), componentName, componentVersion)
	r.NoError(err, "should be able to retrieve component from registry B")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)

	// Verify component does NOT exist in registry A (the original target)
	repoA := connectToOCIRegistry(t, registryA)
	_, err = repoA.GetComponentVersion(t.Context(), componentName, componentVersion)
	r.Error(err, "component should NOT exist in registry A")
}
