package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	"ocm.software/open-component-model/bindings/go/ctf"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_Transfer_OCIImageLayer adds a component version with an OCIImageLayer
// resource that declares no size, transfers it with its resources into an OCI registry,
// and downloads the layer from the target. The layer arrives as a local blob holding the
// raw layer bytes.
func Test_Integration_Transfer_OCIImageLayer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := t.Context()
	r := require.New(t)
	t.Parallel()

	sourceRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start source registry container")
	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start target registry container")
	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: sourceRegistry.Host, Port: sourceRegistry.Port, User: sourceRegistry.User, Password: sourceRegistry.Password},
		{Host: targetRegistry.Host, Port: targetRegistry.Port, User: targetRegistry.User, Password: targetRegistry.Password},
	})
	r.NoError(err)

	const (
		componentName    = "ocm.software/test-oci-image-layer"
		componentVersion = "v1.0.0"
		resourceName     = "chart-layer"
		mediaType        = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	)

	layer := []byte("helm chart layer content")
	layerDigest := digest.FromBytes(layer)
	sourceRepo, err := remote.NewRepository(sourceRegistry.Reference("charts/demo"))
	r.NoError(err)
	sourceRepo.PlainHTTP = true
	sourceRepo.Client = internal.CreateAuthClient(sourceRegistry.RegistryAddress, sourceRegistry.User, sourceRegistry.Password)
	r.NoError(sourceRepo.Blobs().Push(ctx, ociImageSpecV1.Descriptor{
		MediaType: mediaType,
		Digest:    layerDigest,
		Size:      int64(len(layer)),
	}, bytes.NewReader(layer)))

	dir := t.TempDir()
	constructorPath := filepath.Join(dir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(fmt.Sprintf(`components:
- name: %[1]s
  version: %[2]s
  provider:
    name: ocm.software
  resources:
  - name: %[3]s
    version: %[2]s
    type: helmChart
    access:
      type: OCIImageLayer/v1
      ref: http://%[4]s
      mediaType: %[5]s
      digest: %[6]s
`, componentName, componentVersion, resourceName, sourceRegistry.Reference("charts/demo"), mediaType, layerDigest)), 0o600))

	sourceCTF := filepath.Join(dir, "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "creation of component version should succeed")

	fs, err := filesystem.NewFS(sourceCTF, os.O_RDONLY)
	r.NoError(err)
	ctfRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	sourceDesc, err := ctfRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	var sourceAccess v1.OCIImageLayer
	r.NoError(ociaccess.Scheme.Convert(sourceDesc.Component.Resources[0].Access, &sourceAccess))
	r.Equal(int64(len(layer)), sourceAccess.Size, "a missing size is taken from the registry")

	targetRef := fmt.Sprintf("http://%s", targetRegistry.RegistryAddress)
	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion), targetRef,
		"--copy-resources",
		"--config", cfgPath,
	})
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

	targetDesc, err := targetRegistry.Connect(t).GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(targetDesc.Component.Resources, 1)
	accessScheme := ocmruntime.NewScheme(ocmruntime.WithAllowUnknown())
	v2.MustAddToScheme(accessScheme)
	var localBlob v2.LocalBlob
	r.NoError(accessScheme.Convert(targetDesc.Component.Resources[0].Access, &localBlob), "a layer is stored as a local blob")
	r.Equal(mediaType, localBlob.MediaType)

	output := filepath.Join(dir, "downloaded")
	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download", "resource",
		fmt.Sprintf("%s//%s:%s", targetRef, componentName, componentVersion),
		"--identity", fmt.Sprintf("name=%s,version=%s", resourceName, componentVersion),
		"--extraction-policy", "disable",
		"--output", output,
		"--config", cfgPath,
	})
	r.NoError(downloadCMD.ExecuteContext(ctx), "download should succeed")
	data, err := os.ReadFile(output)
	r.NoError(err)
	r.Equal(layer, data, "the downloaded resource is the raw layer")
}
