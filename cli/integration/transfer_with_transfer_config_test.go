package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_TransferWithTransferConfig_CTFToOCI drives a CTF → OCI transfer
// purely via a transfer.config.ocm.software/v1alpha1 wire-format file (no --recursive /
// --copy-resources / --upload-as flags). It proves the load-decode-validate-convert
// path between `--transfer-config <file>` and `transfer.BuildGraphDefinition` is
// wired end-to-end.
func Test_Integration_TransferWithTransferConfig_CTFToOCI(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	componentName := "ocm.software/transfer-config-test"
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

	// Wire-format config. "type" plus the three knobs - the same shape the
	// replication controller will consume from a CRD spec.
	transferCfgYAML := `type: transfer.config.ocm.software/v1alpha1
recursive: 0
copyMode: localBlob
uploadType: default
`
	transferCfgPath := filepath.Join(t.TempDir(), "transfer-config.yaml")
	r.NoError(os.WriteFile(transferCfgPath, []byte(transferCfgYAML), os.ModePerm))

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--transfer-config", transferCfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	r.NoError(transferCMD.ExecuteContext(ctx), "config-driven transfer should succeed")

	repo := connectToOCIRegistry(t, registry)
	desc, err := repo.GetComponentVersion(t.Context(), componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)
}

// Test_Integration_TransferWithTransferConfig_FlagOverridesConfig_DisablesRecursion
// proves the precedence rule with an observable end-state: --recursive=false on
// the command line must override `recursive: true` in --transfer-config.
//
// Setup: a parent component references a child component, each living in its own
// source CTF. The transfer-config asks for recursive=true (which would pull the
// child along), but the CLI is invoked with --recursive=false. If the override
// is honoured, only the parent reaches the target registry; if the override is
// silently dropped, the child also lands and the test fails.
func Test_Integration_TransferWithTransferConfig_FlagOverridesConfig_DisablesRecursion(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	parentComponent := "ocm.software/transfer-config-parent"
	childComponent := "ocm.software/transfer-config-child"
	version := "v1.0.0"

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{{
		Host: registry.Host, Port: registry.Port,
		User: registry.User, Password: registry.Password,
	}})
	r.NoError(err)

	parentConstructor := fmt.Sprintf(`
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

	childConstructor := fmt.Sprintf(`
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

	parentConstructorPath := filepath.Join(t.TempDir(), "parent-constructor.yaml")
	r.NoError(os.WriteFile(parentConstructorPath, []byte(parentConstructor), os.ModePerm))
	childConstructorPath := filepath.Join(t.TempDir(), "child-constructor.yaml")
	r.NoError(os.WriteFile(childConstructorPath, []byte(childConstructor), os.ModePerm))

	// Both components live in the same source CTF, so the transfer command
	// resolves the parent's reference from the source repo without needing a
	// resolver config. Child must be added first so its descriptor exists when
	// parent is registered.
	sourceCTF := filepath.Join(t.TempDir(), "ctf-source")

	addChild := cmd.New()
	addChild.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", childConstructorPath,
	})
	r.NoError(addChild.ExecuteContext(t.Context()), "adding child to source CTF should succeed")

	addParent := cmd.New()
	addParent.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", parentConstructorPath,
	})
	r.NoError(addParent.ExecuteContext(t.Context()), "adding parent to source CTF should succeed")

	transferCfgYAML := `type: transfer.config.ocm.software/v1alpha1
recursive: -1
`
	transferCfgPath := filepath.Join(t.TempDir(), "transfer-config.yaml")
	r.NoError(os.WriteFile(transferCfgPath, []byte(transferCfgYAML), os.ModePerm))

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, parentComponent, version)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--transfer-config", transferCfgPath,
		"--recursive=false",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	r.NoError(transferCMD.ExecuteContext(ctx), "transfer with flag-overridden recursion should succeed")

	repo := connectToOCIRegistry(t, registry)

	parentDesc, err := repo.GetComponentVersion(t.Context(), parentComponent, version)
	r.NoError(err, "parent component must be present in target registry")
	r.Equal(parentComponent, parentDesc.Component.Name)
	r.Equal(version, parentDesc.Component.Version)

	_, err = repo.GetComponentVersion(t.Context(), childComponent, version)
	r.Error(err, "child component must NOT be present in target registry: --recursive=false should have overridden the config's recursive=true")
}

// Test_Integration_TransferWithTransferConfig_InvalidValueRejected ensures the
// loader's Validate() pass rejects bogus enum values cleanly instead of letting
// them flow through to the graph builder. Pre-flight failure is the whole point
// of having a typed wire format.
func Test_Integration_TransferWithTransferConfig_InvalidValueRejected(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	componentName := "ocm.software/transfer-config-invalid-test"
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

	transferCfgYAML := `type: transfer.config.ocm.software/v1alpha1
copyMode: notAValidMode
`
	transferCfgPath := filepath.Join(t.TempDir(), "transfer-config.yaml")
	r.NoError(os.WriteFile(transferCfgPath, []byte(transferCfgYAML), os.ModePerm))

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--transfer-config", transferCfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	err = transferCMD.ExecuteContext(ctx)
	r.Error(err, "invalid copyMode in transfer config should fail before transfer starts")
	r.Contains(err.Error(), "invalid copyMode", "error should identify the invalid field")
}
