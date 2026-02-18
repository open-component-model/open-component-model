package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
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
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_OCIRepository(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting OCI based integration test")

	registry, err := internal.CreateOCIRegistry(t)
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
`, registry.Host, registry.Port, registry.User, registry.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
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
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
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
				fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
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
				fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
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

	t.Run("download resource with decompress flag", func(t *testing.T) {
		originalData := []byte("hello-decompress-test")

		compressedBlob := compression.Compress(direct.NewFromBytes(originalData))

		tests := []struct {
			name               string
			resourceName       string
			component          string
			blob               blob.ReadOnlyBlob
			access             *v2.LocalBlob
			decompress         bool
			expectUncompressed bool // true = output should match originalData
		}{
			{
				name:               "compressed data with --decompress yields original",
				resourceName:       "compressed-resource",
				component:          "ocm.software/test-decompress-on",
				blob:               compressedBlob,
				access:             &v2.LocalBlob{MediaType: "application/octet-stream" + compression.MediaTypeGzipSuffix},
				decompress:         true,
				expectUncompressed: true,
			},
			{
				name:               "compressed data without --decompress stays compressed",
				resourceName:       "compressed-resource-no-flag",
				component:          "ocm.software/test-decompress-off",
				blob:               compressedBlob,
				access:             &v2.LocalBlob{MediaType: "application/octet-stream" + compression.MediaTypeGzipSuffix},
				decompress:         false,
				expectUncompressed: false,
			},
			{
				name:               "uncompressed data with --decompress is a no-op",
				resourceName:       "uncompressed-resource",
				component:          "ocm.software/test-decompress-noop",
				blob:               direct.NewFromBytes(originalData),
				access:             &v2.LocalBlob{},
				decompress:         true,
				expectUncompressed: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := require.New(t)

				version := "v1.0.0"
				localResource := resource{
					Resource: &descriptor.Resource{
						ElementMeta: descriptor.ElementMeta{
							ObjectMeta: descriptor.ObjectMeta{
								Name:    tt.resourceName,
								Version: version,
							},
						},
						Type:         "some-arbitrary-type",
						Access:       tt.access,
						CreationTime: descriptor.CreationTime(time.Now()),
					},
					ReadOnlyBlob: tt.blob,
				}

				uploadComponentVersion(t, repo, tt.component, version, localResource)

				output := filepath.Join(t.TempDir(), "output")

				args := []string{
					"download",
					"resource",
					fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, tt.component, version),
					"--identity",
					fmt.Sprintf("name=%s,version=%s", tt.resourceName, version),
					"--output",
					output,
					"--config",
					cfgPath,
				}
				if tt.decompress {
					args = append(args, "--decompress")
				}

				downloadCMD := cmd.New()
				downloadCMD.SetArgs(args)
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

				if tt.expectUncompressed {
					r.Equal(string(originalData), string(data))
				} else {
					// read the compressed data and ensure it does not match the original uncompressed data
					rc, err := localResource.ReadCloser()
					r.NoError(err)
					t.Cleanup(func() {
						_ = rc.Close()
					})
					compressedData, err := io.ReadAll(rc)
					r.NotEqual(string(originalData), string(data),
						"data should equal to the data in the blob")
					r.Equal(compressedData, data)
				}
			})
		}
	})
}

func Test_Integration_HelmTransformer(t *testing.T) {
	t.Run("upload and download helm chart", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping test in short mode")
		}
		r := require.New(t)

		root := getRepoRootBasedOnGit(t)

		name, version := "ocm.software/helm-chart", "v1.0.0"
		resourceName, resourceVersion := "mychart", "0.1.0"
		constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
		transportArchivePath := filepath.Join(t.TempDir(), "transport-archive")

		// The testing Helm chart should exist at the expected path, we reuse it for our test here
		chartPath := filepath.Join(root, "bindings/go/helm/input/testdata/provenance/mychart-0.1.0.tgz")
		_, err := os.Stat(chartPath)
		r.NoError(err, "Helm chart should exist at %s", chartPath)

		constructor := fmt.Sprintf(`components:
- name: %[1]s
  version: %[2]s
  provider:
    name: acme.org
  resources:
    - name: %[3]s
      version: %[4]s
      type: helmChart
      input:
        type: helm/v1
        path: %[5]s
`, name, version, resourceName, resourceVersion, chartPath)
		r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm), "constructor file must be written without error")

		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", transportArchivePath,
			"--constructor", constructorPath,
		})
		r.NoError(addCMD.ExecuteContext(t.Context()), "adding the component-version to the repository must succeed")

		output := filepath.Join(t.TempDir(), "downloaded-transformed-chart")
		downloadCMD := cmd.New()
		downloadCMD.SetArgs([]string{
			"download",
			"resource",
			fmt.Sprintf("%s//%s:%s", transportArchivePath, name, version),
			"--identity",
			fmt.Sprintf("name=%s,version=%s", resourceName, resourceVersion),
			"--output",
			output,
			"--transformer",
			"helm",
		})
		r.NoError(downloadCMD.ExecuteContext(t.Context()), "downloading and transforming the resource must succeed")

		downloaded, err := os.Stat(output)
		r.NoError(err, "the output directory must exist")
		r.True(downloaded.IsDir(), "the output is a directory that was automatically extracted by the command and transformed")

		entries, err := os.ReadDir(output)
		r.NoError(err, "reading output directory must succeed")
		r.Len(entries, 2, "the output directory should contain exactly two files for the chart and the provenance file")

		chartFile, _ := mustFindChartAndProv(t, output)

		chart, err := loader.LoadFile(chartFile)
		r.NoError(err, "chart should load successfully")
		r.Equal("mychart", chart.Name(), "the chart name should match the resource name")
		r.Equal("0.1.0", chart.Metadata.Version, "the chart version should match the resource version")
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
			resource.Resource, err = repo.UploadResource(ctx, resource.Resource, resource.ReadOnlyBlob, nil)
		}
		r.NoError(err)
		desc.Component.Resources = append(desc.Component.Resources, *resource.Resource)
	}

	r.NoError(repo.AddComponentVersion(ctx, &desc))
}

func mustFindChartAndProv(t *testing.T, dir string) (chartFile, provFile string) {
	t.Helper()
	r := require.New(t)
	ents, err := os.ReadDir(dir)
	r.NoError(err)
	r.Len(ents, 2, "expected chart and .prov only")

	for _, e := range ents {
		switch name := e.Name(); {
		case strings.HasSuffix(name, ".tgz"):
			chartFile = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".prov"):
			provFile = filepath.Join(dir, name)
		}
	}
	r.NotEmpty(chartFile, "chart .tgz missing")
	r.NotEmpty(provFile, ".prov missing")
	return
}

func getRepoRootBasedOnGit(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	git, err := exec.LookPath("git")
	r.NoError(err, "git binary should be available in PATH to build helm input")
	rootRaw, err := exec.CommandContext(t.Context(), git, "rev-parse", "--show-toplevel").Output()
	r.NoError(err, "git rev-parse --show-toplevel must succeed to get repository root")
	return strings.TrimSpace(string(rootRaw))
}
