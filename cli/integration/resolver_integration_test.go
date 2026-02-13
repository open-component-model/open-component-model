package integration

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_ResolverConfig_RoutesComponentsToCorrectRegistry(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	user := "ocm"

	passwordA := internal.GenerateRandomPassword(t, 20)
	htpasswdA := internal.GenerateHtpasswd(t, user, passwordA)
	containerNameA := fmt.Sprintf("resolver-registry-a-%d", time.Now().UnixNano())
	registryA := internal.StartDockerContainerRegistry(t, containerNameA, htpasswdA)
	hostA, portA, err := net.SplitHostPort(registryA)
	r.NoError(err)

	passwordB := internal.GenerateRandomPassword(t, 20)
	htpasswdB := internal.GenerateHtpasswd(t, user, passwordB)
	containerNameB := fmt.Sprintf("resolver-registry-b-%d", time.Now().UnixNano())
	registryB := internal.StartDockerContainerRegistry(t, containerNameB, htpasswdB)
	hostB, portB, err := net.SplitHostPort(registryB)
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
`, hostB, portB, user, passwordB)
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
`, hostA, portA, user, passwordA, hostB, portB, user, passwordB)
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
		"--repository", fmt.Sprintf("http://%s", registryB),
		"--constructor", constructorPathB,
		"--config", cfgPathB,
	})
	r.NoError(addB.ExecuteContext(t.Context()))

	addA := cmd.New()
	addA.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", registryA),
		"--constructor", constructorPathA,
		"--config", resolverCfgPath,
		"--external-component-version-copy-policy", "skip",
		"--skip-reference-digest-processing",
	})
	r.NoError(addA.ExecuteContext(t.Context()))

	getCMD := cmd.New()
	getCMD.SetArgs([]string{
		"get", "component-version",
		fmt.Sprintf("http://%s//%s:%s", registryA, componentA, version),
		"--recursive",
		"--config", resolverCfgPath,
		"--output", "json",
	})

	r.NoError(getCMD.ExecuteContext(t.Context()),
		"get cv --recursive should succeed when resolver config correctly routes component-b to registry-b")
}
