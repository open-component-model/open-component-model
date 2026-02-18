package integration

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	blobfs "ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ociinmemory "ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_Transfer_OCIArtifact_WithLocalBlob(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	// We run this parallel as it spins up a separate container
	t.Parallel()

	// 1. Setup Local OCIRegistry
	sourceRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start source registry container")

	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start target registry container")

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
`, sourceRegistry.Host, sourceRegistry.Port, sourceRegistry.User, sourceRegistry.Password,
		targetRegistry.Host, targetRegistry.Port, targetRegistry.User, targetRegistry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// 3. Create a Source CTF Archive with a component version
	componentName := "ocm.software/test-component"
	componentVersion := "v1.0.0"

	// Create connection to registry for verification later
	client := internal.CreateAuthClient(targetRegistry.RegistryAddress, targetRegistry.User, targetRegistry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(targetRegistry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	// prepare artifact upload
	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, "ghcr.io/test-resource:v1.0.0")
	r.NotNil(access)

	access.Type = ocmruntime.Type{
		Name:    "ociArtifact",
		Version: "v1",
	}

	blob := inmemory.New(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = fmt.Sprintf("http://%s", sourceRegistry.Reference("test-resource:v1.0.0"))
	resource.Access = targetAccess

	resourceRepo := ocires.NewResourceRepository(ociinmemory.New(), ociinmemory.New(), &filesystemv1alpha1.Config{})
	newRes, err := resourceRepo.UploadResource(ctx, &resource, blob, map[string]string{
		"username": sourceRegistry.User,
		"password": sourceRegistry.Password,
	})
	r.NoError(err)
	resource = *newRes

	ociImage, ok := resource.Access.(*v1.OCIImage)
	r.True(ok, "access should be of type OCIImage", "got %T", resource.Access)

	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-resource
    version: v1.0.0
    type: ociArtifact
    access:
      type: %s
      imageReference: %s
`, componentName, componentVersion, ociImage.Type, ociImage.ImageReference)

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
		"--config", cfgPath,
		// "--skip-reference-digest-processing",
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of component version should succeed")

	transferCMD := cmd.New()

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", targetRegistry.RegistryAddress)

	transferCMD.SetArgs([]string{
		"transfer",
		"component-version",
		sourceRef,
		targetRef,
		"--config", cfgPath,
		"--copy-resources", // required, otherwise we wouldn't transfer oci artifacts
		"--upload-as", "localBlob",
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
	var localBlobAccess v2.LocalBlob
	r.NoError(v2.Scheme.Convert(desc.Component.Resources[0].Access, &localBlobAccess))
	r.Equal("test-resource:v1.0.0", localBlobAccess.ReferenceName)
}

func Test_Integration_Transfer_OCIArtifact_AsOCIArtifact(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	// We run this parallel as it spins up a separate container
	t.Parallel()

	// 1. Setup Local OCIRegistry
	sourceRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

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
`, sourceRegistry.Host, sourceRegistry.Port, sourceRegistry.User, sourceRegistry.Password,
		targetRegistry.Host, targetRegistry.Port, targetRegistry.User, targetRegistry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// 3. Create a Source CTF Archive with a component version
	componentName := "ocm.software/test-component"
	componentVersion := "v1.0.0"

	// Create connection to registry for verification later
	client := internal.CreateAuthClient(targetRegistry.RegistryAddress, targetRegistry.User, targetRegistry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(targetRegistry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	// prepare artifact upload
	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, "ghcr.io/test-resource:v1.0.0")
	r.NotNil(access)

	access.Type = ocmruntime.Type{
		Name:    "ociArtifact",
		Version: "v1",
	}

	blob := inmemory.New(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = fmt.Sprintf("http://%s", sourceRegistry.Reference("test-resource:v1.0.0"))
	resource.Access = targetAccess

	resourceRepo := ocires.NewResourceRepository(ociinmemory.New(), ociinmemory.New(), &filesystemv1alpha1.Config{})
	newRes, err := resourceRepo.UploadResource(ctx, &resource, blob, map[string]string{
		"username": sourceRegistry.User,
		"password": sourceRegistry.Password,
	})
	r.NoError(err)
	resource = *newRes

	ociImage, ok := resource.Access.(*v1.OCIImage)
	r.True(ok, "access should be of type OCIImage", "got %T", resource.Access)

	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-resource
    version: v1.0.0
    type: ociArtifact
    access:
      type: %s
      imageReference: %s
`, componentName, componentVersion, ociImage.Type, ociImage.ImageReference)

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
		"--config", cfgPath,
		// "--skip-reference-digest-processing",
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of component version should succeed")

	transferCMD := cmd.New()

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", targetRegistry.RegistryAddress)

	transferCMD.SetArgs([]string{
		"transfer",
		"component-version",
		sourceRef,
		targetRef,
		"--config", cfgPath,
		"--copy-resources",           // required, otherwise we wouldn't transfer oci artifacts
		"--upload-as", "ociArtifact", // This is the new flag we are testing
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

	// Check that the resource access is NOT LocalBlob, but OCIImage (or compatible)
	// and points to the target registry
	resAccess := desc.Component.Resources[0].Access
	// Depending on what AddOCIArtifact produces, it might be OCIImageLayer or ociBlob (legacy)
	// AddOCIArtifact implementation returns LegacyOCIBlobAccessType ("ociBlob")

	_, isLocal := resAccess.(*v2.LocalBlob)
	r.False(isLocal, "resource access should not be LocalBlob")

	// We can try to marshal/unmarshal to check properties if specific type assertion is hard
	rawAccess, err := json.Marshal(resAccess)
	r.NoError(err)

	var accessMap map[string]interface{}
	r.NoError(json.Unmarshal(rawAccess, &accessMap))

	// Check type
	typeVal, ok := accessMap["type"].(string)
	r.Equal("ociArtifact/v1", typeVal, "access type should be ociArtifact")

	// Check reference/imageReference
	// ociBlob has "ref", OCIImage has "imageReference"
	var refVal string
	if v, ok := accessMap["ref"]; ok {
		refVal = v.(string)
	} else if v, ok := accessMap["imageReference"]; ok {
		refVal = v.(string)
	} else {
		r.Fail("Access spec does not contain ref or imageReference")
	}

	// The reference should contain the target registry address
	r.Contains(refVal, targetRegistry.RegistryAddress)
}

// Test_Integration_Transfer_OCIArtifact_PreservesSignatures transfers a signed component
// with OCI artifact resources from CTF to OCI registry and verifies signatures are preserved.
func Test_Integration_Transfer_OCIArtifact_PreservesSignatures(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	t.Parallel()

	sourceRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)
	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

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
`, sourceRegistry.Host, sourceRegistry.Port, sourceRegistry.User, sourceRegistry.Password,
		targetRegistry.Host, targetRegistry.Port, targetRegistry.User, targetRegistry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	componentName := "ocm.software/test-signed-oci-artifact"
	componentVersion := "v1.0.0"

	// Upload OCI artifact to source registry
	originalData := []byte("signed-artifact-data")
	data, access := createSingleLayerOCIImage(t, originalData, "ghcr.io/test-signed:v1.0.0")
	r.NotNil(access)
	access.Type = ocmruntime.Type{Name: "ociArtifact", Version: "v1"}

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test-oci-resource", Version: "v1.0.0"},
		},
		Type:   "some-type",
		Access: access,
	}
	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = fmt.Sprintf("http://%s", sourceRegistry.Reference("test-signed:v1.0.0"))
	resource.Access = targetAccess

	resourceRepo := ocires.NewResourceRepository(ociinmemory.New(), ociinmemory.New(), &filesystemv1alpha1.Config{})
	newRes, err := resourceRepo.UploadResource(ctx, &resource, inmemory.New(bytes.NewReader(data)), map[string]string{
		"username": sourceRegistry.User,
		"password": sourceRegistry.Password,
	})
	r.NoError(err)
	resource = *newRes

	ociImage, ok := resource.Access.(*v1.OCIImage)
	r.True(ok)

	// Build source CTF with signed descriptor via constructor + CLI
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
`, componentName, componentVersion, ociImage.Type, ociImage.ImageReference)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTFPath := filepath.Join(t.TempDir(), "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTFPath),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx))

	// Open the CTF, read descriptor, add signature, re-save
	fs, err := blobfs.NewFS(sourceCTFPath, os.O_RDWR)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	sourceRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	srcDesc, err := sourceRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)

	dig, err := signing.GenerateDigest(ctx, srcDesc, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	srcDesc.Signatures = []descriptor.Signature{
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
	r.NoError(sourceRepo.AddComponentVersion(ctx, srcDesc))

	// Transfer to target OCI registry
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTFPath, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", targetRegistry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--copy-resources",
		"--upload-as", "localBlob",
	})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx))

	// Verify signatures in target
	client := internal.CreateAuthClient(targetRegistry.RegistryAddress, targetRegistry.User, targetRegistry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(targetRegistry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Equal(componentName, desc.Component.Name)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-oci-resource", desc.Component.Resources[0].Name)

	r.Len(desc.Signatures, 1, "signatures should be preserved")
	r.Equal("test-signature", desc.Signatures[0].Name)
	r.Equal(srcDesc.Signatures[0].Digest.HashAlgorithm, desc.Signatures[0].Digest.HashAlgorithm)
	r.Equal(srcDesc.Signatures[0].Digest.Value, desc.Signatures[0].Digest.Value)
	r.Equal("RSASSA-PSS", desc.Signatures[0].Signature.Algorithm)
}

func createSingleLayerOCIImage(t *testing.T, data []byte, ref ...string) ([]byte, *v1.OCIImage) {
	r := require.New(t)
	var buf bytes.Buffer
	w := tar.NewOCILayoutWriter(&buf)

	desc := ociImageSpecV1.Descriptor{}
	desc.Digest = digest.FromBytes(data)
	desc.Size = int64(len(data))
	desc.MediaType = ociImageSpecV1.MediaTypeImageLayer

	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(data)))

	configRaw, err := json.Marshal(map[string]string{})
	r.NoError(err)
	configDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
		MediaType: "application/json",
	}
	r.NoError(w.Push(t.Context(), configDesc, bytes.NewReader(configRaw)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers: []ociImageSpecV1.Descriptor{
			desc,
		},
	}
	manifestRaw, err := json.Marshal(manifest)
	r.NoError(err)

	manifestDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(manifestRaw),
		Size:      int64(len(manifestRaw)),
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
	}
	r.NoError(w.Push(t.Context(), manifestDesc, bytes.NewReader(manifestRaw)))

	for _, ref := range ref {
		r.NoError(w.Tag(t.Context(), manifestDesc, ref))
	}

	r.NoError(w.Close())

	var access *v1.OCIImage

	if len(ref) > 0 {
		access = &v1.OCIImage{
			ImageReference: ref[0],
		}
	}

	return buf.Bytes(), access
}
