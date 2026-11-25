package transformation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	filev1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/helm/transformation"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newConvertTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	descv2.MustAddToScheme(scheme)
	filesystemaccess.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.ConvertHelmToOCI{}, v1alpha1.ConvertHelmToOCIV1alpha1)
	return scheme
}

func TestConvertHelmChartToOCI_Transform(t *testing.T) {
	t.Parallel()

	scheme := newConvertTestScheme()
	testDataDir := filepath.Join("..", "testdata")

	t.Run("converts packaged helm chart to OCI layout", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		chartPath, err := filepath.Abs(filepath.Join(testDataDir, "mychart-0.1.0.tgz"))
		r.NoError(err)

		transform := &transformation.ConvertHelmChartToOCI{
			Scheme: scheme,
		}

		helmAccess := v1.Helm{
			Type: runtime.Type{
				Name:    "helm",
				Version: "v1",
			},
			HelmRepository: "https://example.com",
			HelmChart:      "mychart",
			Version:        "0.1.0",
		}
		rawAccess := runtime.Raw{}
		err = access.Scheme.Convert(&helmAccess, &rawAccess)
		r.NoError(err)

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-convert-helm-to-oci",
			Spec: &v1alpha1.ConvertHelmToOCISpec{
				ChartFile: filev1alpha1.File{
					URI: fileURI(t, chartPath),
				},
				Resource: &descv2.Resource{
					ElementMeta: descv2.ElementMeta{
						ObjectMeta: descv2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type:   "helmChart",
					Access: &rawAccess,
				},
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		output, ok := result.(*v1alpha1.ConvertHelmToOCI)
		r.True(ok)
		r.NotNil(output.Output)
		r.NotNil(output.Output.Resource)
		r.NotNil(output.Output.Resource.Digest)
		r.NotEmpty(output.Output.Resource.Digest.Value)
		r.Equal("SHA-256", output.Output.Resource.Digest.HashAlgorithm)
		r.Equal("ociArtifactDigest/v1", output.Output.Resource.Digest.NormalisationAlgorithm)

		// Verify the output file was created
		ociPath := filePath(t, output.Output.File.URI)
		assert.FileExists(t, ociPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(ociPath)
		})

		// Only the output file stays behind: the layout is built in a temporary file first.
		leftovers, err := filepath.Glob(filepath.Join(outputDir, "oci-layout-*"))
		r.NoError(err)
		r.Empty(leftovers)

		// Verify it is a valid OCI layout with a helm chart layer
		verifyOCILayout(t, ociPath, "mychart", "0.1.0", false)

		// Verify resource metadata is passed through
		assert.Equal(t, "mychart", output.Output.Resource.Name)
		assert.Equal(t, "0.1.0", output.Output.Resource.Version)
	})

	t.Run("converts packaged helm chart with provenance to OCI layout", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		chartPath, err := filepath.Abs(filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz"))
		r.NoError(err)
		provPath, err := filepath.Abs(filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz.prov"))
		r.NoError(err)

		transform := &transformation.ConvertHelmChartToOCI{
			Scheme: scheme,
		}

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": "https://example.com",
			"helmChart":      "mychart:0.1.0",
		}, runtime.JSONOptions)
		r.NoError(err)

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-convert-helm-to-oci-prov",
			Spec: &v1alpha1.ConvertHelmToOCISpec{
				ChartFile: filev1alpha1.File{
					URI: fileURI(t, chartPath),
				},
				ProvFile: &filev1alpha1.File{
					URI: fileURI(t, provPath),
				},
				Resource: &descv2.Resource{
					ElementMeta: descv2.ElementMeta{
						ObjectMeta: descv2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{
							Name:    "helm",
							Version: "v1",
						},
						Data: helmAccessData,
					},
				},
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		output, ok := result.(*v1alpha1.ConvertHelmToOCI)
		r.True(ok)
		r.NotNil(output.Output)
		r.NotNil(output.Output.Resource)
		r.NotNil(output.Output.Resource.Digest)
		r.NotEmpty(output.Output.Resource.Digest.Value)

		ociPath := filePath(t, output.Output.File.URI)
		assert.FileExists(t, ociPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(ociPath)
		})

		verifyOCILayout(t, ociPath, "mychart", "0.1.0", true)

		// Verify provenance can be cryptographically verified
		verifyProvenance(t, ociPath,
			filepath.Join(testDataDir, "provenance", "pub.gpg"),
			"testkey",
			"mychart-0.1.0.tgz",
		)
	})

	t.Run("creates output in temp dir when no output path specified", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		chartPath, err := filepath.Abs(filepath.Join(testDataDir, "mychart-0.1.0.tgz"))
		r.NoError(err)

		transform := &transformation.ConvertHelmChartToOCI{
			Scheme: scheme,
		}

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": "https://example.com",
			"helmChart":      "mychart:0.1.0",
		}, runtime.JSONOptions)
		r.NoError(err)

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-convert-no-output-path",
			Spec: &v1alpha1.ConvertHelmToOCISpec{
				ChartFile: filev1alpha1.File{
					URI: fileURI(t, chartPath),
				},
				Resource: &descv2.Resource{
					ElementMeta: descv2.ElementMeta{
						ObjectMeta: descv2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{
							Name:    "helm",
							Version: "v1",
						},
						Data: helmAccessData,
					},
				},
				// OutputPath intentionally empty
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		output, ok := result.(*v1alpha1.ConvertHelmToOCI)
		r.True(ok)
		r.NotNil(output.Output)
		r.NotNil(output.Output.Resource)
		r.NotNil(output.Output.Resource.Digest)
		r.NotEmpty(output.Output.Resource.Digest.Value)

		ociPath := filePath(t, output.Output.File.URI)
		assert.FileExists(t, ociPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(ociPath)
		})

		verifyOCILayout(t, ociPath, "mychart", "0.1.0", false)
	})

	t.Run("removes the buffered layout when a later step fails", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()
		tempDir := t.TempDir()

		chartPath, err := filepath.Abs(filepath.Join(testDataDir, "mychart-0.1.0.tgz"))
		r.NoError(err)

		transform := &transformation.ConvertHelmChartToOCI{
			Scheme:  scheme,
			TempDir: tempDir,
		}

		// Without a chart name the OCI image reference cannot be constructed, which fails
		// after the layout has been written to the buffered file.
		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": "https://example.com",
			"version":        "0.1.0",
		}, runtime.JSONOptions)
		r.NoError(err)

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-convert-cleanup-on-error",
			Spec: &v1alpha1.ConvertHelmToOCISpec{
				ChartFile: filev1alpha1.File{
					URI: fileURI(t, chartPath),
				},
				Resource: &descv2.Resource{
					ElementMeta: descv2.ElementMeta{
						ObjectMeta: descv2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{
							Name:    "helm",
							Version: "v1",
						},
						Data: helmAccessData,
					},
				},
			},
		}

		_, err = transform.Transform(ctx, spec)
		r.Error(err)

		entries, err := os.ReadDir(tempDir)
		r.NoError(err)
		r.Empty(entries, "the buffered layout must not survive a failed transformation")
	})

	t.Run("reports the chart layer", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			chartPath string
			provPath  string
		}{
			{
				name:      "without provenance",
				chartPath: filepath.Join(testDataDir, "mychart-0.1.0.tgz"),
			},
			{
				name:      "with provenance",
				chartPath: filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz"),
				provPath:  filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz.prov"),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				r := require.New(t)

				chartPath, err := filepath.Abs(tt.chartPath)
				r.NoError(err)
				chartData, err := os.ReadFile(chartPath)
				r.NoError(err)
				chartSum := sha256.Sum256(chartData)

				helmAccess := v1.Helm{
					Type:           runtime.Type{Name: "helm", Version: "v1"},
					HelmRepository: "https://example.com",
					HelmChart:      "mychart",
					Version:        "0.1.0",
				}
				rawAccess := runtime.Raw{}
				r.NoError(access.Scheme.Convert(&helmAccess, &rawAccess))

				spec := &v1alpha1.ConvertHelmToOCI{
					Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
					ID:   "test-chart-layer",
					Spec: &v1alpha1.ConvertHelmToOCISpec{
						ChartFile: filev1alpha1.File{URI: fileURI(t, chartPath)},
						Resource: &descv2.Resource{
							ElementMeta: descv2.ElementMeta{
								ObjectMeta: descv2.ObjectMeta{Name: "mychart", Version: "0.1.0"},
							},
							Type:   "helmChart",
							Access: &rawAccess,
						},
						OutputPath: t.TempDir(),
					},
				}
				if tt.provPath != "" {
					provPath, err := filepath.Abs(tt.provPath)
					r.NoError(err)
					spec.Spec.ProvFile = &filev1alpha1.File{URI: fileURI(t, provPath)}
				}

				transform := &transformation.ConvertHelmChartToOCI{Scheme: scheme}
				result, err := transform.Transform(t.Context(), spec)
				r.NoError(err)
				output, ok := result.(*v1alpha1.ConvertHelmToOCI)
				r.True(ok)
				r.NotNil(output.Output)
				t.Cleanup(func() {
					_ = os.RemoveAll(filePath(t, output.Output.File.URI))
				})

				r.Equal(v1alpha1.ChartLayer{
					MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
					Digest:    "sha256:" + hex.EncodeToString(chartSum[:]),
					Size:      int64(len(chartData)),
				}, output.Output.ChartLayer)
			})
		}
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.ConvertHelmChartToOCI{
			Scheme: scheme,
		}

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-nil-spec",
			Spec: nil,
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "spec is required")
	})
}

// verifyOCILayout reads the OCI layout file at the given path and verifies it
// contains a valid helm chart manifest with expected layers.
func verifyOCILayout(t *testing.T, ociPath, expectedName, expectedVersion string, expectProv bool) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	chartBlob, err := openFileAsBlob(ociPath)
	r.NoError(err)

	store, err := ocitar.ReadOCILayout(ctx, chartBlob)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(store.Close())
	})
	r.Len(store.Index.Manifests, 1)

	manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(manifestRaw.Close())
	})

	manifest := ociImageSpecV1.Manifest{}
	r.NoError(json.UnmarshalRead(manifestRaw, &manifest))

	// Verify config layer
	assert.Equal(t, registry.ConfigMediaType, manifest.Config.MediaType)

	// Verify config content
	configRaw, err := store.Fetch(ctx, manifest.Config)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(configRaw.Close())
	})
	var config map[string]string
	r.NoError(json.UnmarshalRead(configRaw, &config))
	assert.Equal(t, expectedName, config["name"])
	assert.Equal(t, expectedVersion, config["version"])

	// Verify chart layer
	r.GreaterOrEqual(len(manifest.Layers), 1)
	assert.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType)

	if expectProv {
		r.Len(manifest.Layers, 2, "expected chart and provenance layers")
		assert.Equal(t, registry.ProvLayerMediaType, manifest.Layers[1].MediaType)
	}

	// Verify tag matches version
	desc := store.Index.Manifests[0]
	assert.Equal(t, expectedVersion, desc.Annotations[ociImageSpecV1.AnnotationRefName])
}

// verifyProvenance verifies the provenance layer in the OCI layout can be
// cryptographically verified against the given GPG keyring.
func verifyProvenance(t *testing.T, ociPath, gpgKeyPath, keyID, chartFileName string) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	chartBlob, err := openFileAsBlob(ociPath)
	r.NoError(err)

	store, err := ocitar.ReadOCILayout(ctx, chartBlob)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(store.Close())
	})

	manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(manifestRaw.Close())
	})

	manifest := ociImageSpecV1.Manifest{}
	r.NoError(json.UnmarshalRead(manifestRaw, &manifest))
	r.Len(manifest.Layers, 2)

	chartLayer, err := store.Fetch(ctx, manifest.Layers[0])
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(chartLayer.Close())
	})
	chartData, err := io.ReadAll(chartLayer)
	r.NoError(err)

	provLayer, err := store.Fetch(ctx, manifest.Layers[1])
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(provLayer.Close())
	})
	provData, err := io.ReadAll(provLayer)
	r.NoError(err)

	signatory, err := provenance.NewFromKeyring(gpgKeyPath, keyID)
	r.NoError(err)

	_, err = signatory.Verify(chartData, provData, chartFileName)
	r.NoError(err, "provenance verification failed")
}

// openFileAsBlob opens a file and wraps it as a simple ReadOnlyBlob for use with ReadOCILayout.
type fileBlob struct {
	path string
}

func openFileAsBlob(path string) (*fileBlob, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return &fileBlob{path: path}, nil
}

func (f *fileBlob) ReadCloser() (io.ReadCloser, error) {
	return os.Open(f.path)
}

// fileURI is the file:// URI of the absolute path p.
func fileURI(t *testing.T, p string) string {
	t.Helper()
	uri, err := filesystem.URIFromFilePath(p)
	require.NoError(t, err)
	return uri
}
