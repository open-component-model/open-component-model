package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// writeOCMConfigWithCredsAndTransfer composes the central OCM config used by these tests:
// credentials for one or more registries plus a single transfer.config.ocm.software/v1alpha1
// entry. Inlining the YAML mirrors the pattern used elsewhere in this package (e.g.
// transfer_helm_integration_test.go) instead of growing the shared helper for one caller.
func writeOCMConfigWithCredsAndTransfer(t *testing.T, regs []internal.ConfigOpts, transferEntry string) string {
	t.Helper()

	cfg := "\ntype: generic.config.ocm.software/v1\nconfigurations:\n- type: credentials.config.ocm.software\n  consumers:"
	for _, o := range regs {
		cfg += fmt.Sprintf(`
  - identity:
      type: OCIRegistry
      hostname: %q
      port: %q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q`, o.Host, o.Port, o.User, o.Password)
	}
	cfg += "\n" + transferEntry + "\n"

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))
	t.Logf("Generated config:%s", cfg)
	return cfgPath
}

// Test_Integration_TransferWithTransferConfig_FileDrivesCopyMode proves that
// `copyMode: allResources` set purely as a transfer.config.ocm.software/v1alpha1
// entry in the central OCM configuration (no flags) actually reaches the
// transfer engine.
//
// The signal: a source component has an external `OCIImage` access pointing at
// the source registry. With the default `copyMode: localBlob`, the access stays
// untouched in the target descriptor (it would still point at the source
// registry). With `copyMode: allResources`, the resource is fetched and re-stored
// as a `LocalBlob` in the target. The assertion fails if the entry is silently
// dropped.
func Test_Integration_TransferWithTransferConfig_FileDrivesCopyMode(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	t.Parallel()

	sourceRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start source registry container")

	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start target registry container")

	// Drive copyMode purely from the central OCM config. No --copy-resources flag.
	cfgPath := writeOCMConfigWithCredsAndTransfer(t, []internal.ConfigOpts{
		{Host: sourceRegistry.Host, Port: sourceRegistry.Port, User: sourceRegistry.User, Password: sourceRegistry.Password},
		{Host: targetRegistry.Host, Port: targetRegistry.Port, User: targetRegistry.User, Password: targetRegistry.Password},
	}, `- type: transfer.config.ocm.software/v1alpha1
  copyMode: allResources
  uploadType: localBlob`)

	componentName := "ocm.software/transfer-config-copymode-test"
	componentVersion := "v1.0.0"

	// Push an OCI image to the source registry and build a CTF component that
	// references it via OCIImage access.
	originalData := []byte("transfer-config copyMode payload")
	imageData := internal.CreateSingleLayerOCIImageLayoutTar(t, originalData, "ghcr.io/transfer-config-copymode:v1.0.0").Bytes()

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test-oci-resource", Version: "v1.0.0"},
		},
		Type: "ociArtifact",
		Access: &v1.OCIImage{
			Type:           ocmruntime.Type{Name: "ociArtifact", Version: "v1"},
			ImageReference: fmt.Sprintf("http://%s", sourceRegistry.Reference("transfer-config-copymode:v1.0.0")),
		},
	}

	resourceRepo := ocires.NewResourceRepository(&filesystemv1alpha1.Config{})
	uploaded, err := resourceRepo.UploadResource(ctx, &resource, inmemory.New(bytes.NewReader(imageData)), &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: sourceRegistry.User,
		Password: sourceRegistry.Password,
	})
	r.NoError(err, "should upload OCI artifact to source registry")
	resource = *uploaded

	uploadedAccess, ok := resource.Access.(*v1.OCIImage)
	r.True(ok, "uploaded access should remain OCIImage")

	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-oci-resource
    version: v1.0.0
    type: ociArtifact
    access:
      type: %s
      imageReference: %s
`, componentName, componentVersion, uploadedAccess.Type, uploadedAccess.ImageReference)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "creating source CTF should succeed")

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", targetRegistry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
	})

	transferCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	r.NoError(transferCMD.ExecuteContext(transferCtx), "config-driven transfer should succeed")

	repo := targetRegistry.Connect(t)
	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "transferred component must be present in target registry")
	r.Len(desc.Component.Resources, 1)

	gotAccess := desc.Component.Resources[0].Access
	r.NotNil(gotAccess, "resource access must not be nil")

	// The whole point of the test: with localBlob copyMode (the default), this
	// would still be OCIImage pointing at the source registry. With the central
	// config's allResources honored, the resource was fetched and re-stored locally.
	r.Equal(v2.LocalBlobAccessType, gotAccess.GetType().Name,
		"resource access must be LocalBlob: copyMode: allResources from the OCM config was not honored")
}

// Test_Integration_TransferWithTransferConfig_FlagOverridesFileRecursion
// proves the override branch in buildGraphDefinitionFromArgs: a recursion
// setting in the central OCM config can be overridden by an explicit
// --recursive flag, and that override wins.
//
// The signal: a parent component references a child component, both in one
// source CTF. The config says `recursive: 0` (no recursion) and the CLI passes
// `--recursive`. With the override honored, the child component lands in the
// target.
func Test_Integration_TransferWithTransferConfig_FlagOverridesFileRecursion(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	parentComponent := "ocm.software/transfer-config-recursive-parent"
	childComponent := "ocm.software/transfer-config-recursive-child"
	version := "v1.0.0"

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	// Config asks for no recursion; flag asks for full recursion. The flag wins.
	cfgPath := writeOCMConfigWithCredsAndTransfer(t, []internal.ConfigOpts{{
		Host: registry.Host, Port: registry.Port,
		User: registry.User, Password: registry.Password,
	}}, `- type: transfer.config.ocm.software/v1alpha1
  recursive: 0`)

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

	// Both components live in the same source CTF so the transfer command
	// resolves the parent's reference without a resolver config. Child must be
	// registered first so its descriptor exists when parent is registered.
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

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, parentComponent, version)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--recursive",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	r.NoError(transferCMD.ExecuteContext(ctx), "transfer with flag-overridden recursion should succeed")

	repo := registry.Connect(t)

	parentDesc, err := repo.GetComponentVersion(t.Context(), parentComponent, version)
	r.NoError(err, "parent component must be present in target registry")
	r.Equal(parentComponent, parentDesc.Component.Name)

	// The whole point of the test: with the config's `recursive: 0` honored, the
	// child would not land. Only because --recursive overrode the config does the
	// child reach the target.
	childDesc, err := repo.GetComponentVersion(t.Context(), childComponent, version)
	r.NoError(err, "child component must be present in target registry: --recursive flag did not override the config's recursive: 0")
	r.Equal(childComponent, childDesc.Component.Name)
}

// Test_Integration_TransferWithTransferConfig_InvalidValueRejected ensures the
// LookupConfig Validate() pass rejects bogus enum values cleanly instead of
// letting them flow through to the graph builder. Pre-flight failure is the
// whole point of having a typed wire format.
func Test_Integration_TransferWithTransferConfig_InvalidValueRejected(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	componentName := "ocm.software/transfer-config-invalid-test"
	componentVersion := "v1.0.0"

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	cfgPath := writeOCMConfigWithCredsAndTransfer(t, []internal.ConfigOpts{{
		Host: registry.Host, Port: registry.Port,
		User: registry.User, Password: registry.Password,
	}}, `- type: transfer.config.ocm.software/v1alpha1
  copyMode: notAValidMode`)

	sourceRef := createSourceCTF(t, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	err = transferCMD.ExecuteContext(ctx)
	r.Error(err, "invalid copyMode in transfer config should fail before transfer starts")
	r.Contains(err.Error(), "invalid copyMode", "error should identify the invalid field")
}
