package integration

import (
	"bytes"
	"context"
	"crypto"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/signing"
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

// Test_Integration_TransferComponentVersion_PreservesSignatures_ToOCI verifies that signatures
// on a component descriptor are preserved when transferring a component version with local blob
// resources to an OCI registry. This exercises the OCIAddComponentVersion transformer path.
func Test_Integration_TransferComponentVersion_PreservesSignatures_ToOCI(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	// 1. Setup target OCI registry
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

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

	// 2. Create a signed source CTF with a local blob resource using the Go API
	componentName := "ocm.software/test-signed-to-oci"
	componentVersion := "1.0.0"

	fromDesc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider:  descriptor.Provider{Name: "ocm.software"},
			Resources: []descriptor.Resource{},
		},
	}

	// Add a local blob resource
	fromDesc.Component.Resources = []descriptor.Resource{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-blob",
					Version: "1.0.0",
				},
			},
			Type:     "plainText",
			Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				MediaType: "text/plain",
			},
		},
	}

	// Generate a digest and add a signature
	dig, err := signing.GenerateDigest(t.Context(), fromDesc, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	fromDesc.Signatures = []descriptor.Signature{
		{
			Name:   "test-signature",
			Digest: *dig,
			Signature: descriptor.SignatureInfo{
				Algorithm: "RSASSA-PSS",
				Value:     "dGVzdC1zaWduYXR1cmUtdmFsdWU=",
				MediaType: "application/vnd.ocm.signature.rsa",
			},
		},
	}

	// Setup source CTF
	sourceCTFPath := filepath.Join(t.TempDir(), "source-ctf")
	fs, err := filesystem.NewFS(sourceCTFPath, os.O_RDWR|os.O_CREATE)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	sourceRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	ctx := t.Context()

	// Add local blob resource data
	blobData := []byte("Hello, signed world for OCI!")
	updatedRes, err := sourceRepo.AddLocalResource(
		ctx, componentName, componentVersion,
		&fromDesc.Component.Resources[0],
		inmemory.New(bytes.NewReader(blobData)),
	)
	r.NoError(err)
	fromDesc.Component.Resources[0] = *updatedRes

	// Add the signed component version
	r.NoError(sourceRepo.AddComponentVersion(ctx, fromDesc))

	// 3. Transfer to OCI registry with --copy-resources
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTFPath, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--copy-resources",
	})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer to OCI registry should succeed")

	// 4. Verify signatures in target OCI registry
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	targetDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component from OCI registry")
	r.Equal(componentName, targetDesc.Component.Name)
	r.Equal(componentVersion, targetDesc.Component.Version)

	// Verify signatures were preserved
	r.Len(targetDesc.Signatures, 1, "transferred descriptor should have 1 signature")
	r.Equal("test-signature", targetDesc.Signatures[0].Name)
	r.Equal(fromDesc.Signatures[0].Digest.HashAlgorithm, targetDesc.Signatures[0].Digest.HashAlgorithm)
	r.Equal(fromDesc.Signatures[0].Digest.Value, targetDesc.Signatures[0].Digest.Value)
	r.Equal("RSASSA-PSS", targetDesc.Signatures[0].Signature.Algorithm)
	r.Equal("dGVzdC1zaWduYXR1cmUtdmFsdWU=", targetDesc.Signatures[0].Signature.Value)

	// Verify resource was also transferred
	r.Len(targetDesc.Component.Resources, 1)
	r.Equal("test-blob", targetDesc.Component.Resources[0].Name)
}
