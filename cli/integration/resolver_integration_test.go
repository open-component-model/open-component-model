package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_ResolverConfig_RoutesComponentsToCorrectRegistry(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	registryA, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	registryB, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	componentA := "ocm.software/resolver-test/component-a"
	componentB := "ocm.software/resolver-test/component-b"
	version := "v1.0.0"

	cfgB := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: %q
      port: %q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q
`, registryB.Host, registryB.Port, registryB.User, registryB.Password)
	cfgPathB := filepath.Join(t.TempDir(), "ocmconfig-b.yaml")
	r.NoError(os.WriteFile(cfgPathB, []byte(cfgB), os.ModePerm))

	resolverCfg := fmt.Sprintf(`
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
  - identity:
      type: OCIRepository
      hostname: %[5]q
      port: %[6]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[7]q
        password: %[8]q
- type: resolvers.config.ocm.software
  resolvers:
  - repository:
      type: OCIRepository/v1
      baseUrl: http://%[5]s:%[6]s
    componentNamePattern: "ocm.software/resolver-test/component-b"
`, registryA.Host, registryA.Port, registryA.User, registryA.Password, registryB.Host, registryB.Port, registryB.User, registryB.Password)
	resolverCfgPath := filepath.Join(t.TempDir(), "ocmconfig-resolver.yaml")
	r.NoError(os.WriteFile(resolverCfgPath, []byte(resolverCfg), os.ModePerm))

	constructorA := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  componentReferences:
    - name: ref-to-b
      version: %s
      componentName: %s
  resources:
  - name: resource-a
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "resource from component-a"
`, componentA, version, version, componentB)

	constructorB := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: resource-b
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "resource from component-b"
`, componentB, version)

	constructorPathA := filepath.Join(t.TempDir(), "constructor-a.yaml")
	r.NoError(os.WriteFile(constructorPathA, []byte(constructorA), os.ModePerm))
	constructorPathB := filepath.Join(t.TempDir(), "constructor-b.yaml")
	r.NoError(os.WriteFile(constructorPathB, []byte(constructorB), os.ModePerm))

	addB := cmd.New()
	addB.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", registryB.RegistryAddress),
		"--constructor", constructorPathB,
		"--config", cfgPathB,
	})
	r.NoError(addB.ExecuteContext(t.Context()))

	addA := cmd.New()
	addA.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", registryA.RegistryAddress),
		"--constructor", constructorPathA,
		"--config", resolverCfgPath,
		"--external-component-version-copy-policy", "skip",
		"--skip-reference-digest-processing",
	})
	r.NoError(addA.ExecuteContext(t.Context()))

	output := new(bytes.Buffer)
	getCMD := cmd.New()
	getCMD.SetOut(output)
	getCMD.SetArgs([]string{
		"get", "component-version",
		fmt.Sprintf("http://%s//%s:%s", registryA.RegistryAddress, componentA, version),
		"--recursive",
		"--config", resolverCfgPath,
		"--output", "json",
	})

	r.NoError(getCMD.ExecuteContext(t.Context()),
		"get cv --recursive should succeed when resolver config correctly routes component-b to registry-b")

	strOutput := output.String()
	r.Contains(strOutput, "ocm.software/resolver-test/component-a", "output should contain resource from component-a")
	r.Contains(strOutput, "ocm.software/resolver-test/component-b", "output should contain resource from component-b")
	r.Contains(strOutput, fmt.Sprintf("%s/component-descriptors/ocm.software/resolver-test/component-a:v1.0.0", registryA.RegistryAddress), "output should contain reference to component-a in registry-a")
	r.Contains(strOutput, fmt.Sprintf("%s/component-descriptors/ocm.software/resolver-test/component-b:v1.0.0", registryB.RegistryAddress), "output should contain reference to component-b in registry-b")
}
