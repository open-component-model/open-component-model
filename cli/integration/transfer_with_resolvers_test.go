package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
)

func Test_Integration_TransferWithResolvers_CTF(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	parentComponent := "ocm.software/parent"
	childComponent := "ocm.software/child"
	version := "v1.0.0"

	constructorParent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  componentReferences:
    - name: child-ref
      version: %s
      componentName: %s
  resources:
  - name: parent-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "parent resource content"
`, parentComponent, version, version, childComponent)

	constructorChild := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: child-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "child resource content"
`, childComponent, version)

	constructorParentPath := filepath.Join(t.TempDir(), "constructor-parent.yaml")
	r.NoError(os.WriteFile(constructorParentPath, []byte(constructorParent), os.ModePerm))

	constructorChildPath := filepath.Join(t.TempDir(), "constructor-child.yaml")
	r.NoError(os.WriteFile(constructorChildPath, []byte(constructorChild), os.ModePerm))

	ctfA := filepath.Join(t.TempDir(), "ctf-a")
	ctfB := filepath.Join(t.TempDir(), "ctf-b")
	ctfC := filepath.Join(t.TempDir(), "ctf-c")

	addChild := cmd.New()
	addChild.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfB),
		"--constructor", constructorChildPath,
	})
	r.NoError(addChild.ExecuteContext(t.Context()))

	resolverCfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: resolvers.config.ocm.software
  resolvers:
  - repository:
      type: CommonTransportFormat/v1
      filePath: %s
    componentNamePattern: "%s"
  - repository:
      type: CommonTransportFormat/v1
      filePath: %s
    componentNamePattern: "%s"
`, ctfA, parentComponent, ctfB, childComponent)

	resolverCfgPath := filepath.Join(t.TempDir(), "resolver-config.yaml")
	r.NoError(os.WriteFile(resolverCfgPath, []byte(resolverCfg), os.ModePerm))

	addParent := cmd.New()
	addParent.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfA),
		"--constructor", constructorParentPath,
		"--skip-reference-digest-processing",
		"--external-component-version-copy-policy", "skip",
		"--config", resolverCfgPath,
	})
	r.NoError(addParent.ExecuteContext(t.Context()))

	transferCMD := cmd.New()
	transferOutput := new(bytes.Buffer)
	transferCMD.SetOut(transferOutput)
	transferCMD.SetErr(transferOutput)
	transferCMD.SetArgs([]string{
		"transfer",
		"component-version",
		fmt.Sprintf("ctf::%s//%s:%s", ctfA, parentComponent, version),
		fmt.Sprintf("ctf::%s", ctfC),
		"--recursive",
		"--config", resolverCfgPath,
	})

	transferErr := transferCMD.ExecuteContext(t.Context())
	r.NoError(transferErr, "transfer failed with error: %s", transferOutput.String())

	//errorOutput := transferOutput.String()
	//r.Contains(errorOutput, "failed getting local resource",
	//	"error output should contain 'failed getting local resource'")
}
