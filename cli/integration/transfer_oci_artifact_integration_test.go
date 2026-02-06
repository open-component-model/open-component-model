package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ociinmemory "ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

type mockCredentialsResolver struct {
	Username string
	Password string
}

func (m *mockCredentialsResolver) Resolve(ctx context.Context, identity ocmruntime.Identity) (map[string]string, error) {
	return map[string]string{
		"username": m.Username,
		"password": m.Password,
	}, nil
}

func Test_Integration_Transfer_OCIArtifact(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	// We run this parallel as it spins up a separate container
	t.Parallel()

	// 1. Setup Local Registry
	user := "ocm"
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)

	containerName := fmt.Sprintf("source-oci-artifact-repository-%d", time.Now().UnixNano())
	registryAddress := internal.StartDockerContainerRegistry(t, containerName, htpasswd)
	host, port, err := net.SplitHostPort(registryAddress)
	r.NoError(err)

	targetContainerName := fmt.Sprintf("target-oci-artifact-repository-%d", time.Now().UnixNano())
	targetRegistryAddress := internal.StartDockerContainerRegistry(t, targetContainerName, htpasswd)
	targetHost, targetPort, err := net.SplitHostPort(targetRegistryAddress)
	r.NoError(err)

	reference := func(ref string) string {
		return fmt.Sprintf("%s/%s", registryAddress, ref)
	}

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
        username: %[3]q
        password: %[4]q
`, host, port, user, password,
		targetHost, targetPort)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// 3. Create a Source CTF Archive with a component version
	componentName := "ocm.software/test-component"
	componentVersion := "v1.0.0"

	// Create connection to registry for verification later
	client := internal.CreateAuthClient(targetRegistryAddress, user, password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(targetRegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	// prepare artifact upload
	credsResolver := mockCredentialsResolver{
		Username: user,
		Password: password,
	}

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
	targetAccess.(*v1.OCIImage).ImageReference = fmt.Sprintf("http://%s", reference("test-resource:v1.0.0"))
	resource.Access = targetAccess

	creds, err := credsResolver.Resolve(ctx, nil)
	r.NoError(err)
	r.NotNil(creds)

	resourceRepo := ocires.NewResourceRepository(ociinmemory.New(), ociinmemory.New(), &filesystemv1alpha1.Config{})
	newRes, err := resourceRepo.UploadResource(ctx, &resource, blob, creds)
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
		"--skip-reference-digest-processing",
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of component version should succeed")

	transferCMD := cmd.New()

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", targetRegistryAddress)

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

	// 5. Verification
	// Check if component exists in target registry
	desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)
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
