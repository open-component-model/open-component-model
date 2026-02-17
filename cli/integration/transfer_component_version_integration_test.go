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

func Test_Integration_TransferComponentVersion(t *testing.T) {
	r := require.New(t)
	// We run this parallel as it spins up a separate container
	t.Parallel()

	// 1. Setup Local OCIRegistry
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	// 2. Configure OCM to point to this registry
	// We create a temporary ocmconfig.yaml
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
`, registry.Host, registry.Port, registry.User, registry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// 3. Create a Source CTF Archive with a component version
	componentName := "ocm.software/test-component"
	componentVersion := "v1.0.0"

	// Create connection to registry for verification later
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	// We can use the 'add component-version' command to create a CTF archive easily
	// Or we manually construct one using constructor.yaml and 'add component-version' command targetting a ctf path

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
      text: "Hello, World from Transfer Test!"
`, componentName, componentVersion)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")

	// Create source CTF
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of source CTF should succeed")

	// 4. Run Transfer Command: CTF -> OCI OCIRegistry
	transferCMD := cmd.New()

	// Construct source ref: ctf::<path>//<component>:<version>
	// Because the "add" command creates a CTF structure, we can reference it directly.
	// The "add" command with ctf repository puts it into the directory.
	// We need to verify if "add" creates a valid repository structure on the fly or if we need to init it.
	// The previous add_component_version_integration_test.go suggests it works directly.

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD.SetArgs([]string{
		"transfer",
		"component-version",
		sourceRef,
		targetRef,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Executes transfer
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

	// 5. Verification
	// Check if component exists in target registry
	desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)
}
