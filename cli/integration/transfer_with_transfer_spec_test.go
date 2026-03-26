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

// Test_Integration_TransferWithTransferSpec_CTFToOCI performs a two-step transfer:
// 1. Generate the transfer spec via --dry-run
// 2. Execute the spec via --transfer-spec
func Test_Integration_TransferWithTransferSpec_CTFToOCI(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	// 1. Setup target OCI registry
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{{
		Host: registry.Host, Port: registry.Port,
		User: registry.User, Password: registry.Password,
	}})
	r.NoError(err)

	// 2. Create source CTF archive
	componentName := "ocm.software/transfer-spec-test"
	componentVersion := "v1.0.0"

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

	// 3. Generate transfer spec via --dry-run
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

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

	// 4. Write spec to file and execute via --transfer-spec
	specFile := filepath.Join(t.TempDir(), "transfer-spec.yaml")
	r.NoError(os.WriteFile(specFile, specOutput.Bytes(), os.ModePerm))

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		"--transfer-spec", specFile,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	r.NoError(transferCMD.ExecuteContext(ctx), "transfer from spec should succeed")

	// 5. Verify component exists in target registry
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
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

	// 1. Setup two OCI registries
	registryA, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry A")

	registryB, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry B")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: registryA.Host, Port: registryA.Port, User: registryA.User, Password: registryA.Password},
		{Host: registryB.Host, Port: registryB.Port, User: registryB.User, Password: registryB.Password},
	})
	r.NoError(err)

	// 2. Create source CTF archive
	componentName := "ocm.software/modified-target-spec-test"
	componentVersion := "v1.0.0"

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
      text: "Hello from modified-target transfer-spec test!"
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

	// 3. Generate transfer spec targeting registry A
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRefA := fmt.Sprintf("http://%s", registryA.RegistryAddress)

	dryRunCMD := cmd.New()
	specOutput := new(bytes.Buffer)
	dryRunCMD.SetOut(specOutput)
	dryRunCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRefA,
		"--config", cfgPath,
		"--dry-run", "-o", "yaml",
	})
	r.NoError(dryRunCMD.ExecuteContext(t.Context()), "dry-run should succeed")

	// 4. Modify the spec to point to registry B instead of registry A
	modifiedSpec := strings.ReplaceAll(specOutput.String(), registryA.RegistryAddress, registryB.RegistryAddress)
	r.NotEqual(specOutput.String(), modifiedSpec, "spec should contain registry A address to replace")

	specFile := filepath.Join(t.TempDir(), "transfer-spec.yaml")
	r.NoError(os.WriteFile(specFile, []byte(modifiedSpec), os.ModePerm))

	// 5. Execute the modified spec
	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		"--transfer-spec", specFile,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	r.NoError(transferCMD.ExecuteContext(ctx), "transfer from modified spec should succeed")

	// 6. Verify component exists in registry B (the modified target)
	clientB := internal.CreateAuthClient(registryB.RegistryAddress, registryB.User, registryB.Password)
	resolverB, err := urlresolver.New(
		urlresolver.WithBaseURL(registryB.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(clientB),
	)
	r.NoError(err)
	targetRepoB, err := oci.NewRepository(oci.WithResolver(resolverB), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	desc, err := targetRepoB.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve component from registry B")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)

	// 7. Verify component does NOT exist in registry A (the original target)
	clientA := internal.CreateAuthClient(registryA.RegistryAddress, registryA.User, registryA.Password)
	resolverA, err := urlresolver.New(
		urlresolver.WithBaseURL(registryA.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(clientA),
	)
	r.NoError(err)
	targetRepoA, err := oci.NewRepository(oci.WithResolver(resolverA), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	_, err = targetRepoA.GetComponentVersion(ctx, componentName, componentVersion)
	r.Error(err, "component should NOT exist in registry A")
}
