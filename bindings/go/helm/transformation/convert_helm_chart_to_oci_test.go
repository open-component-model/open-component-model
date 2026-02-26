package transformation_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"

	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	filev1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
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
					URI: "file://" + chartPath,
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

		// Verify the output file was created
		ociPath := strings.TrimPrefix(output.Output.File.URI, "file://")
		assert.FileExists(t, ociPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(ociPath)
		})

		// Verify it is a valid OCI layout with a helm chart layer
		verifyOCILayout(t, ociPath, "mychart", "0.1.0", false)

		// Verify resource metadata is passed through
		assert.Equal(t, "mychart", output.Output.Resource.Name)
		assert.Equal(t, "0.1.0", output.Output.Resource.Version)
	})

	t.Run("converts packaged helm chart with provenance to OCI layout", func(t *testing.T) {
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
		})
		r.NoError(err)

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-convert-helm-to-oci-prov",
			Spec: &v1alpha1.ConvertHelmToOCISpec{
				ChartFile: filev1alpha1.File{
					URI: "file://" + chartPath,
				},
				ProvFile: &filev1alpha1.File{
					URI: "file://" + provPath,
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

		ociPath := strings.TrimPrefix(output.Output.File.URI, "file://")
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
		})
		r.NoError(err)

		spec := &v1alpha1.ConvertHelmToOCI{
			Type: runtime.NewVersionedType(v1alpha1.ConvertHelmToOCIType, v1alpha1.Version),
			ID:   "test-convert-no-output-path",
			Spec: &v1alpha1.ConvertHelmToOCISpec{
				ChartFile: filev1alpha1.File{
					URI: "file://" + chartPath,
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

		ociPath := strings.TrimPrefix(output.Output.File.URI, "file://")
		assert.FileExists(t, ociPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(ociPath)
		})

		verifyOCILayout(t, ociPath, "mychart", "0.1.0", false)
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
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
	r.NoError(json.NewDecoder(manifestRaw).Decode(&manifest))

	// Verify config layer
	assert.Equal(t, registry.ConfigMediaType, manifest.Config.MediaType)

	// Verify config content
	configRaw, err := store.Fetch(ctx, manifest.Config)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(configRaw.Close())
	})
	var config map[string]string
	r.NoError(json.NewDecoder(configRaw).Decode(&config))
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
	r.NoError(json.NewDecoder(manifestRaw).Decode(&manifest))
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
