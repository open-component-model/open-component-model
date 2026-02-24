package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"

	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/transformation"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// testdataHelmChartPath returns the absolute path to the test helm chart .tgz.
func testdataHelmChartPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok, "failed to get caller info")
	return filepath.Join(filepath.Dir(filename), "..", "input", "testdata", "provenance", "mychart-0.1.0.tgz")
}

func Test_Integration_GetHelmChart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// Load real helm chart test data
	chartPath := testdataHelmChartPath(t)
	chartData, err := os.ReadFile(chartPath)
	require.NoError(t, err, "failed to read test helm chart")

	// Create an in-memory blob from the chart data
	chartBlob := inmemory.New(bytes.NewReader(chartData))
	chartBlob.SetMediaType("application/vnd.cncf.helm.chart.content.v1.tar+gzip")

	// Set up a combined scheme with all required type registrations
	combinedScheme := ocmruntime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.GetHelmChart{}, v1alpha1.GetHelmChartV1alpha1)

	t.Run("downloads OCI Helm chart and transforms to blob", func(t *testing.T) {
		r := require.New(t)
		resourceRepo := resource.NewResourceRepository(&filesystemv1alpha1.Config{})
	}

	t.Run("downloads helm chart and transforms to blob", func(t *testing.T) {
		r := require.New(t)

		transform := &transformation.GetHelmChart{
			Scheme:     combinedScheme,
			Repository: mockRepo,
		}

		spec := &v1alpha1.GetHelmChart{
			Type: ocmruntime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-get-helm-chart",
			Spec: &v1alpha1.GetHelmChartSpec{
				Resource: &v2.Resource{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &ocmruntime.Raw{
						Type: ocmruntime.Type{
							Name:    "helm",
							Version: "v1",
						},
					},
				},
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		helmOutput, ok := result.(*v1alpha1.GetHelmChart)
		r.True(ok)
		r.NotNil(helmOutput.Output)
		r.NotNil(helmOutput.Output.Resource)

		// Verify output file was created
		outputPath := strings.TrimPrefix(helmOutput.Output.File.URI, "file://")
		assert.FileExists(t, outputPath)
		t.Cleanup(func() {
			_ = os.Remove(outputPath)
		})

		// Verify the file content is a valid helm chart
		chrt, err := loader.Load(outputPath)
		r.NoError(err)
		r.NotNil(chrt)

		accessor, err := chart.NewAccessor(chrt)
		r.NoError(err)
		assert.Equal(t, "mychart", accessor.Name())

		// Verify output resource metadata
		assert.Equal(t, "mychart", helmOutput.Output.Resource.Name)
		assert.Equal(t, "0.1.0", helmOutput.Output.Resource.Version)
	})

	t.Run("downloads helm chart to specified output path", func(t *testing.T) {
		r := require.New(t)
		outputDir := t.TempDir()

		transform := &transformation.GetHelmChart{
			Scheme:     combinedScheme,
			Repository: mockRepo,
		}

		spec := &v1alpha1.GetHelmChart{
			Type: ocmruntime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-get-helm-chart-output-path",
			Spec: &v1alpha1.GetHelmChartSpec{
				Resource: &v2.Resource{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &ocmruntime.Raw{
						Type: ocmruntime.Type{
							Name:    "helm",
							Version: "v1",
						},
					},
				},
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		helmOutput, ok := result.(*v1alpha1.GetHelmChart)
		r.True(ok)
		r.NotNil(helmOutput.Output)

		// Verify file was created in the specified output directory
		outputPath := strings.TrimPrefix(helmOutput.Output.File.URI, "file://")
		assert.FileExists(t, outputPath)
		assert.True(t, strings.HasPrefix(outputPath, outputDir))

		// Verify the content is still a valid helm chart
		chrt, err := loader.Load(outputPath)
		r.NoError(err)

		accessor, err := chart.NewAccessor(chrt)
		r.NoError(err)
		assert.Equal(t, "mychart", accessor.Name())
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
		r := require.New(t)

		transform := &transformation.GetHelmChart{
			Scheme:     combinedScheme,
			Repository: mockRepo,
		}

		spec := &v1alpha1.GetHelmChart{
			Type: ocmruntime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-nil-spec",
			Spec: nil,
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "spec is required")
	})
}
