package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/cli/cmd"
	resourceCMD "ocm.software/open-component-model/cli/cmd/download/resource"
	"ocm.software/open-component-model/cli/cmd/download/resource/integration/internal"
)

func Test_Integration_OCIRepository(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting OCI based integration test")
	user := "ocm"

	// Setup credentials and htpasswd
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)

	// Start containerized registry
	registryAddress := internal.StartDockerContainerRegistry(t, htpasswd)
	host, port, err := net.SplitHostPort(registryAddress)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository/v1
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`, host, port, user, password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	client := internal.CreateAuthClient(registryAddress, user, password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	t.Run("download resource with arbitrary byte stream data", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-foobar",
						Version: "v1.0.0",
					},
				},
				Type:         "some-arbitrary-type-packed-in-image",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("foobar")),
		}

		name, version := "ocm.software/test-component", "v1.0.0"

		uploadComponentVersion(t, repo, name, version, localResource)

		downloadCMD := cmd.New()

		output := filepath.Join(t.TempDir(), "image-layout")

		downloadCMD.SetArgs([]string{
			"download",
			"resource",
			fmt.Sprintf("http://%s//%s:%s", registryAddress, name, version),
			"--identity",
			fmt.Sprintf("name=%s,version=%s", localResource.Resource.Name, localResource.Resource.Version),
			"--output",
			output,
			"--config",
			cfgPath,
		})
		r.NoError(downloadCMD.ExecuteContext(t.Context()))

		outputBlob, err := filesystem.GetBlobFromOSPath(output)
		r.NoError(err)

		dataStream, err := outputBlob.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(dataStream.Close())
		})

		data, err := io.ReadAll(dataStream)
		r.NoError(err)

		r.Equal("foobar", string(data), "Downloaded data should match the original data")
	})

	t.Run("download resource containing oci image layout", func(t *testing.T) {
		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "image-layout",
						Version: "v1.0.0",
					},
				},
				Type: "some-arbitrary-type-packed-in-image",
				Access: &v2.LocalBlob{
					MediaType: layout.MediaTypeOCIImageLayoutTarV1,
				},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBuffer(
				internal.CreateSingleLayerOCIImageLayoutTar(t, []byte("foobar"), "myimage:v1.0.0"),
				true),
		}

		name, version := "ocm.software/test-component", "v1.0.0"

		uploadComponentVersion(t, repo, name, version, localResource)

		t.Run("download with disabled extract", func(t *testing.T) {
			r := require.New(t)
			output := filepath.Join(t.TempDir(), "image-layout")
			downloadCMD := cmd.New()
			downloadCMD.SetArgs([]string{
				"download",
				"resource",
				fmt.Sprintf("http://%s//%s:%s", registryAddress, name, version),
				"--identity",
				fmt.Sprintf("name=%s,version=%s", localResource.Resource.Name, localResource.Resource.Version),
				"--output",
				output,
				"--config",
				cfgPath,
				"--extraction-policy",
				resourceCMD.ExtractionPolicyDisable,
			})
			r.NoError(downloadCMD.ExecuteContext(t.Context()))

			fi, err := os.Stat(output)
			r.NoError(err)
			r.False(fi.IsDir(), "the output is a tar that was not automatically extracted by the command")
		})

		t.Run("download with auto extract and read TAR", func(t *testing.T) {
			r := require.New(t)
			output := filepath.Join(t.TempDir(), "image-layout")
			downloadCMD := cmd.New()
			downloadCMD.SetArgs([]string{
				"download",
				"resource",
				fmt.Sprintf("http://%s//%s:%s", registryAddress, name, version),
				"--identity",
				fmt.Sprintf("name=%s,version=%s", localResource.Resource.Name, localResource.Resource.Version),
				"--output",
				output,
				"--config",
				cfgPath,
			})
			r.NoError(downloadCMD.ExecuteContext(t.Context()))

			fi, err := os.Stat(output)
			r.NoError(err)
			r.True(fi.IsDir(), "the output is a tar^ that was automatically extracted by the command")

			idx := filepath.Join(output, "index.json")
			idxData, err := os.ReadFile(idx)
			r.NoError(err)
			var index ociImageSpecV1.Index
			r.NoError(json.Unmarshal(idxData, &index))
			r.Len(index.Manifests, 1)

			store, err := orasoci.NewFromFS(t.Context(), os.DirFS(output))
			r.NoError(err)

			_, data, err := oras.FetchBytes(t.Context(), store, index.Manifests[0].Digest.String(), oras.FetchBytesOptions{})
			r.NoError(err)
			var manifest ociImageSpecV1.Manifest
			r.NoError(json.Unmarshal(data, &manifest))
			r.Len(manifest.Layers, 1)

			_, layerData, err := oras.FetchBytes(t.Context(), store, manifest.Layers[0].Digest.String(), oras.FetchBytesOptions{})
			r.NoError(err)
			r.Equal("foobar", string(layerData))
		})
	})
}

type resource struct {
	*descriptor.Resource
	blob.ReadOnlyBlob
}

func uploadComponentVersion(t *testing.T, repo repository.ComponentVersionRepository, name, version string,
	resources ...resource,
) {
	ctx := t.Context()
	r := require.New(t)

	desc := descriptor.Descriptor{}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Labels = append(desc.Component.Labels, descriptor.Label{Name: "foo", Value: []byte(`"bar"`)})
	desc.Component.Provider.Name = "ocm.software"

	for _, resource := range resources {
		var err error
		switch resource.Resource.GetAccess().(type) {
		case *v2.LocalBlob:
			resource.Resource, err = repo.AddLocalResource(ctx, name, version, resource.Resource, resource.ReadOnlyBlob)
		default:
			repo, ok := repo.(repository.ResourceRepository)
			r.True(ok, "repository must implement ResourceRepository to upload global accesses")
			resource.Resource, err = repo.UploadResource(ctx, resource.Resource, resource.ReadOnlyBlob)
		}
		r.NoError(err)
		desc.Component.Resources = append(desc.Component.Resources, *resource.Resource)
	}

	r.NoError(repo.AddComponentVersion(ctx, &desc))
}
