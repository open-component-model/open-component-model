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
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ociinmemory "ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_Transfer_OCIArtifact(t *testing.T) {
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

	// Set up repository provider and credential resolver to check whether the
	// transfers worked as expected.
	repoProvider := provider.NewComponentVersionRepositoryProvider()
	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	r.NoError(err)
	credconf, err := runtime.LookupCredentialConfig(ocmconf)
	r.NoError(err)
	credentialResolver, err := credentials.ToGraph(ctx, credconf, credentials.Options{
		RepositoryPluginProvider: credentials.GetRepositoryPluginFn(func(ctx context.Context, typed ocmruntime.Typed) (credentials.RepositoryPlugin, error) {
			return nil, fmt.Errorf("no repository plugin configured for type %s", typed.GetType().String())
		}),
		CredentialPluginProvider: credentials.GetCredentialPluginFn(func(ctx context.Context, typed ocmruntime.Typed) (credentials.CredentialPlugin, error) {
			return nil, fmt.Errorf("no credential plugin configured for type %s", typed.GetType().String())
		}),
		CredentialRepositoryTypeScheme: ocmruntime.NewScheme(),
	})
	r.NoError(err)

	// 3. Create a Source CTF Archive with a component version
	componentName := "ocm.software/test-component"
	componentVersion := "v1.0.0"

	// prepare artifact upload
	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, "ghcr.io/test-oci-resource:v1.0.0")
	r.NotNil(access)

	access.Type = ocmruntime.Type{
		Name:    "ociArtifact",
		Version: "v1",
	}

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-oci-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = fmt.Sprintf("http://%s", sourceRegistry.Reference("test-oci-resource:v1.0.0"))
	resource.Access = targetAccess

	// Upload the resource to the source oci registry where the constructor
	// image reference is pointing to.
	resourceRepo := ocires.NewResourceRepository(ociinmemory.New(), ociinmemory.New(), &filesystemv1alpha1.Config{})
	id, err := resourceRepo.GetResourceCredentialConsumerIdentity(ctx, &resource)
	r.NoError(err, "should be able to get credential consumer identity for resource")
	creds, err := credentialResolver.Resolve(ctx, id)
	r.NoError(err, "should be able to resolve credentials for resource")
	blob := inmemory.New(bytes.NewReader(data))
	newRes, err := resourceRepo.UploadResource(ctx, &resource, blob, creds)
	r.NoError(err)
	resource = *newRes

	// Store the resource to the file system to be used in the constructor
	// to add a local blob oci layout
	tempdir := t.TempDir()
	ociLayoutPath := filepath.Join(tempdir, "oci-layout")
	r.NoError(os.WriteFile(ociLayoutPath, data, os.ModePerm))

	var ociArtifactAccess v1.OCIImage
	r.NoError(ociaccess.Scheme.Convert(resource.Access, &ociArtifactAccess), "should be able to convert access to OCIImage")

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
  - name: test-localblob-oci-resource
    version: v1.0.0
    type: ociArtifact
    input:
      type: file
      path: %s
      mediaType: %s
`, componentName, componentVersion, ociArtifactAccess.Type, ociArtifactAccess.ImageReference, ociLayoutPath, layout.MediaTypeOCIImageLayoutV1)

	constructorPath := filepath.Join(tempdir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(tempdir, "source-ctf")

	// Create source CTF
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of component version should succeed")

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)

	t.Run("transfer with default (no --upload-as flag)", func(t *testing.T) {
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "default")

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			sourceRef,
			targetRef,
			"--config", cfgPath,
			"--copy-resources", // required, otherwise we wouldn't transfer oci artifacts
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		// Executes transfer
		r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

		// Set up a repository to download components from the target to check whether
		// the transfer worked as expected.
		targetRepo, err := createTargetOCIRepo(ctx, repoProvider, credentialResolver, targetRef)
		r.NoError(err, "should be able to create target repository")

		// Check if component exists in target registry
		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 2)

		r.Equal("test-oci-resource", desc.Component.Resources[0].Name)
		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[0].Access, &localBlobAccess))
		r.Equal("test-oci-resource:v1.0.0", localBlobAccess.ReferenceName)

		r.Equal("test-localblob-oci-resource", desc.Component.Resources[1].Name)
		var localBlobAccess2 v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[1].Access, &localBlobAccess2))
	})

	t.Run("transfer with --upload-as localBlob", func(t *testing.T) {
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "as/local")

		transferCMD := cmd.New()
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

		// Set up a repository to download components from the target to check whether
		// the transfer worked as expected.
		targetRepo, err := createTargetOCIRepo(ctx, repoProvider, credentialResolver, targetRef)

		// Check if component exists in target registry
		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal("test-oci-resource", desc.Component.Resources[0].Name)
		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[0].Access, &localBlobAccess))
		r.Equal("test-oci-resource:v1.0.0", localBlobAccess.ReferenceName)
	})

	t.Run("transfer with --upload-as ociArtifact", func(t *testing.T) {
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "as/oci")

		transferCMD := cmd.New()
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

		// Set up a repository to download components from the target to check whether
		// the transfer worked as expected.
		targetRepo, err := createTargetOCIRepo(ctx, repoProvider, credentialResolver, targetRef)
		r.NoError(err, "should be able to create target repository")

		// Check if component exists in target registry
		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal("test-oci-resource", desc.Component.Resources[0].Name)

		var ociAccess v1.OCIImage
		r.NoError(ociaccess.Scheme.Convert(desc.Component.Resources[0].Access, &ociAccess))
		r.Equal(fmt.Sprintf("%s/test-oci-resource:v1.0.0", targetRef), ociAccess.ImageReference)
	})
}

func createTargetOCIRepo(ctx context.Context, repoProvider *provider.CachingComponentVersionRepositoryProvider, credentialResolver credentials.Resolver, targetRef string) (repository.ComponentVersionRepository, error) {
	targetSpec := &ociv1.Repository{
		BaseUrl: targetRef,
	}
	id, err := repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, targetSpec)
	creds, err := credentialResolver.Resolve(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("should be able to resolve credentials for target repository: %w", err)
	}
	targetRepo, err := repoProvider.GetComponentVersionRepository(ctx, targetSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("should be able to get repository for target: %w", err)
	}
	return targetRepo, nil
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
