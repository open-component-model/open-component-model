package integration

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob"
	blobfs "ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/cmd/configuration"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
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
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: OCIRegistry
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

	tempdir := t.TempDir()
	cfgPath := filepath.Join(tempdir, "ocmconfig.yaml")
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
	resourceRepo := ocires.NewResourceRepository(&filesystemv1alpha1.Config{})
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
`, componentName, componentVersion, ociArtifactAccess.Type, ociArtifactAccess.ImageReference, ociLayoutPath, layout.MediaTypeOCIImageLayoutTarV1)

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
		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
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
		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})

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

	t.Run("transfer with --upload-as ociArtifact (local blob with missing reference name)", func(t *testing.T) {
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "as/oci/norefname")

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
		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err, "should be able to create target repository")

		// Check if component exists in target registry
		desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 2)
		r.Equal("test-oci-resource", desc.Component.Resources[0].Name)

		var ociAccess v1.OCIImage
		r.NoError(ociaccess.Scheme.Convert(desc.Component.Resources[0].Access, &ociAccess))
		r.Equal(fmt.Sprintf("%s/test-oci-resource:v1.0.0", targetRef), ociAccess.ImageReference)

		// upload as oci is expected to be skipped due to missing reference name
		r.Equal("test-localblob-oci-resource", desc.Component.Resources[1].Name)
		var localBlobAccess2 v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[1].Access, &localBlobAccess2))
	})

	t.Run("transfer with --upload-as ociArtifact", func(t *testing.T) {
		// Perform an intermediary transfer to get a local blob with a reference name
		intermediaryctf := filepath.Join(tempdir, "intermediary-ctf")
		intermediaryRef := fmt.Sprintf("ctf::%s", intermediaryctf)
		transferIntermediaryCMD := cmd.New()
		transferIntermediaryCMD.SetArgs([]string{
			"transfer",
			"component-version",
			sourceRef,
			intermediaryRef,
			"--config", cfgPath,
			"--copy-resources", // required, otherwise we wouldn't transfer oci artifacts
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		// Executes transfer
		r.NoError(transferIntermediaryCMD.ExecuteContext(ctx), "transfer should succeed")

		// Check that the intermediary looks as we expect
		intermediaryRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ctfv1.Repository{FilePath: intermediaryctf})
		r.NoError(err, "should be able to create intermediary repository")
		desc, err := intermediaryRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve component from intermediary repository")
		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[0].Access, &localBlobAccess))
		r.Equal("test-oci-resource:v1.0.0", localBlobAccess.ReferenceName)
		var anotherLocalBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[1].Access, &anotherLocalBlobAccess))
		r.Equal("", anotherLocalBlobAccess.ReferenceName)

		// Actual transfer to be tested
		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, "as/oci/refname")
		intermediaryRef = fmt.Sprintf("ctf::%s//%s:%s", intermediaryctf, componentName, componentVersion)

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			intermediaryRef,
			targetRef,
			"--config", cfgPath,
			"--copy-resources",           // required, otherwise we wouldn't transfer oci artifacts
			"--upload-as", "ociArtifact", // This is the new flag we are testing
		})

		// Executes transfer
		r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

		// Set up a repository to download components from the target to check whether
		// the transfer worked as expected.
		targetRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err, "should be able to create target repository")

		// Check if component exists in target registry
		desc, err = targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 2)
		r.Equal("test-oci-resource", desc.Component.Resources[0].Name)

		var ociAccess v1.OCIImage
		r.NoError(ociaccess.Scheme.Convert(desc.Component.Resources[0].Access, &ociAccess))
		r.Equal(fmt.Sprintf("%s/test-oci-resource:v1.0.0", targetRef), ociAccess.ImageReference)

		// upload as oci is expected to be skipped due to missing reference name
		r.Equal("test-localblob-oci-resource", desc.Component.Resources[1].Name)
		var localBlobAccess2 v2.LocalBlob
		r.NoError(v2.Scheme.Convert(desc.Component.Resources[1].Access, &localBlobAccess2))
	})
}

func createRepo(ctx context.Context, repoProvider *provider.CachingComponentVersionRepositoryProvider, credentialResolver credentials.Resolver, targetSpec ocmruntime.Typed) (repository.ComponentVersionRepository, error) {
	var creds ocmruntime.Typed
	id, err := repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, targetSpec)
	if err == nil {
		creds, err = credentialResolver.Resolve(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("should be able to resolve credentials for target repository: %w", err)
		}
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
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: OCIRegistry
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

	resourceRepo := ocires.NewResourceRepository(&filesystemv1alpha1.Config{})
	newRes, err := resourceRepo.UploadResource(ctx, &resource, inmemory.New(bytes.NewReader(data)), &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: sourceRegistry.User,
		Password: sourceRegistry.Password,
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

// Regression for #3674 and legacy v2 OCI-root digests labeled genericBlobDigest/v1:
// copying resources must preserve signed descriptors and parent reference digests.
func Test_Integration_Transfer_OCIArtifact_PreservesV1DescriptorDigest(t *testing.T) {
	r := require.New(t)
	t.Parallel()
	ctx := t.Context()

	sourceRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)
	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: sourceRegistry.Host, Port: sourceRegistry.Port, User: sourceRegistry.User, Password: sourceRegistry.Password},
		{Host: targetRegistry.Host, Port: targetRegistry.Port, User: targetRegistry.User, Password: targetRegistry.Password},
	})
	r.NoError(err)
	key := mustRSAKey(t)
	cfg, err := os.ReadFile(cfgPath)
	r.NoError(err)
	cfg = append(cfg, []byte(fmt.Sprintf(`
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %q
        private_key_pem: %q
`, pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))...)
	r.NoError(os.WriteFile(cfgPath, cfg, 0o600))

	sourceRepo := sourceRegistry.Connect(t)
	resourceRepo := ocires.NewResourceRepository(&filesystemv1alpha1.Config{})
	originalData := []byte("issue-3674-artifact")
	data, access := createSingleLayerOCIImage(t, originalData,
		"http://"+sourceRegistry.Reference("issue-3674-artifact:1.0.0"))
	access.Type = ocmruntime.NewVersionedType("ociArtifact", "v1")

	resource, err := resourceRepo.UploadResource(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "artifact", Version: "1.0.0"},
		},
		Type:     "ociImage",
		Relation: descriptor.LocalRelation,
		Access:   access,
	}, inmemory.New(bytes.NewReader(data)), &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: sourceRegistry.User,
		Password: sourceRegistry.Password,
	})
	r.NoError(err)
	r.NotNil(resource.Digest)

	r.Equal("ociArtifactDigest/v1", resource.Digest.NormalisationAlgorithm, "new OCI resources must use the OCI digest by default")
	fixtureRoot, err := tar.CopyOCILayoutWithIndex(ctx, memory.New(), inmemory.New(bytes.NewReader(data)), tar.CopyOCILayoutWithIndexOptions{})
	r.NoError(err)
	r.Equal(fixtureRoot.Digest.Encoded(), resource.Digest.Value)
	r.NotEqual(digest.FromBytes(data).Encoded(), resource.Digest.Value, "legacy compatibility must verify the OCI root, not the tar blob hash")

	for _, algorithm := range []string{"ociArtifactDigest/v1", "genericBlobDigest/v1"} {
		for _, route := range []string{"registry-to-registry", "registry-ctf-registry"} {
			for _, uploadAs := range []string{"ociArtifact", "localBlob"} {
				t.Run(algorithm+"/"+route+"/"+uploadAs, func(t *testing.T) {
					r := require.New(t)
					ctx := t.Context()
					run := func(args ...string) {
						t.Helper()
						command := cmd.New()
						command.SetArgs(append(args, "--config", cfgPath))
						ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
						r.NoError(command.ExecuteContext(ctx))
					}
					// Distinct identities prevent an earlier transfer from satisfying a later case.
					name := fmt.Sprintf("ocm.software/issue3674/%s/%s/%s", algorithm, route, uploadAs)
					name = strings.ToLower(name)
					const version = "1.0.0"
					const creationTime = "2026-06-19T07:16:30Z"
					child := &descriptor.Descriptor{
						Meta: descriptor.Meta{Version: "v2"},
						Component: descriptor.Component{
							ComponentMeta: descriptor.ComponentMeta{
								ObjectMeta:   descriptor.ObjectMeta{Name: name + "/child", Version: version},
								CreationTime: creationTime,
							},
							Provider:  descriptor.Provider{Name: "ocm.software"},
							Resources: []descriptor.Resource{*resource.DeepCopy()},
						},
					}
					// Legacy v2 writers stored the OCI root under the generic blob label.
					// Finalize this metadata before signing; transfer must not migrate it.
					child.Component.Resources[0].Digest.NormalisationAlgorithm = algorithm
					child.Component.Resources[0].CreationTime = descriptor.CreationTime(time.Date(2026, time.June, 19, 7, 16, 30, 0, time.UTC))
					wantResourceDigest := child.Component.Resources[0].Digest.DeepCopy()
					sourceRef := "http://" + sourceRegistry.RegistryAddress
					ref := func(d *descriptor.Descriptor) string { return "//" + d.Component.Name + ":" + version }
					r.NoError(sourceRepo.AddComponentVersion(ctx, child))
					run("sign", "cv", sourceRef+ref(child))
					child, err := sourceRepo.GetComponentVersion(ctx, child.Component.Name, version)
					r.NoError(err)
					r.Len(child.Signatures, 1)
					childDigest, err := signing.GenerateDigest(ctx, child, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
					r.NoError(err)
					parent := &descriptor.Descriptor{
						Meta: descriptor.Meta{Version: "v2"},
						Component: descriptor.Component{
							ComponentMeta: descriptor.ComponentMeta{
								ObjectMeta:   descriptor.ObjectMeta{Name: name + "/parent", Version: version},
								CreationTime: creationTime,
							},
							Provider: descriptor.Provider{Name: "ocm.software"},
							References: []descriptor.Reference{{
								ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: version}},
								Component:   child.Component.Name,
								Digest:      *childDigest,
							}},
						},
					}
					r.NoError(sourceRepo.AddComponentVersion(ctx, parent))
					run("sign", "cv", sourceRef+ref(parent))
					parent, err = sourceRepo.GetComponentVersion(ctx, parent.Component.Name, version)
					r.NoError(err)
					r.Len(parent.Signatures, 1)

					assertPreserved := func(repo repository.ComponentVersionRepository, repoRef string, registry *internal.OCIRegistry, local bool) {
						t.Helper()
						got, err := repo.GetComponentVersion(ctx, child.Component.Name, version)
						r.NoError(err)
						gotParent, err := repo.GetComponentVersion(ctx, parent.Component.Name, version)
						r.NoError(err)
						r.Equal(creationTime, got.Component.CreationTime)
						r.Equal(creationTime, gotParent.Component.CreationTime)
						r.Len(got.Component.Resources, 1)
						r.Equal(child.Component.Resources[0].CreationTime, got.Component.Resources[0].CreationTime)
						r.Equal(wantResourceDigest, got.Component.Resources[0].Digest)
						r.Equal(child.Signatures, got.Signatures)
						r.Equal(parent.Signatures, gotParent.Signatures)
						r.Equal(parent.Component.References, gotParent.Component.References)
						gotDigest, err := signing.GenerateDigest(ctx, got, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
						r.NoError(err)
						r.Equal(gotParent.Component.References[0].Digest, *gotDigest, "the signed parent reference must still verify")
						run("verify", "cv", repoRef+ref(child))
						run("verify", "cv", repoRef+ref(parent))

						var content blob.ReadOnlyBlob
						if local {
							var localAccess v2.LocalBlob
							r.NoError(v2.Scheme.Convert(got.Component.Resources[0].Access, &localAccess))
							r.NotEmpty(localAccess.ReferenceName)
							content, _, err = repo.GetLocalResource(ctx, got.Component.Name, version, got.Component.Resources[0].ToIdentity())
						} else {
							var targetAccess v1.OCIImage
							r.NoError(ociaccess.Scheme.Convert(got.Component.Resources[0].Access, &targetAccess))
							r.Contains(targetAccess.ImageReference, registry.RegistryAddress+"/")
							content, err = resourceRepo.DownloadResource(ctx, &got.Component.Resources[0], &ocicredsv1.OCICredentials{
								Type:     ocicredsv1.OCICredentialsVersionedType,
								Username: registry.User, Password: registry.Password,
							})
						}
						r.NoError(err)
						store := memory.New()
						root, err := tar.CopyOCILayoutWithIndex(ctx, store, content, tar.CopyOCILayoutWithIndexOptions{})
						r.NoError(err)
						r.Equal(fixtureRoot.Digest, root.Digest)
						reader, err := store.Fetch(ctx, ociImageSpecV1.Descriptor{
							Digest: digest.FromBytes(originalData), Size: int64(len(originalData)), MediaType: ociImageSpecV1.MediaTypeImageLayer,
						})
						r.NoError(err)
						copiedData, err := io.ReadAll(reader)
						r.NoError(reader.Close())
						r.NoError(err)
						r.Equal(originalData, copiedData)
					}
					assertPreserved(sourceRepo, sourceRef, sourceRegistry, false)
					transfer := func(from, to string) {
						t.Helper()
						run("transfer", "cv", from+ref(parent), to, "--recursive", "--copy-resources", "--upload-as", uploadAs)
					}
					from := sourceRef
					if route == "registry-ctf-registry" {
						ctfPath := filepath.Join(t.TempDir(), "intermediary")
						ctfRef := "ctf::" + ctfPath
						transfer(from, ctfRef)
						fs, err := blobfs.NewFS(ctfPath, os.O_RDONLY)
						r.NoError(err)
						ctfRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
						r.NoError(err)
						assertPreserved(ctfRepo, ctfRef, nil, true)
						from = ctfRef
					}
					targetRef := "http://" + targetRegistry.RegistryAddress
					transfer(from, targetRef)
					assertPreserved(targetRegistry.Connect(t), targetRef, targetRegistry, uploadAs == "localBlob")
				})
			}
		}
	}
}

func createSingleLayerOCIImage(t *testing.T, data []byte, ref ...string) ([]byte, *v1.OCIImage) {
	r := require.New(t)
	var buf bytes.Buffer
	w, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

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
