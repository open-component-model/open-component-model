package integration

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/input"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_InputMethodProcessResourceRemoteChartPodinfo(t *testing.T) {
	testCases := []struct {
		name     string
		resource *constructorruntime.Resource
		error    func(t *testing.T, err error) bool
	}{
		{
			name: "remote chart with version https",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "internal.charts.example.com/charts:6.9.1",
						HelmRepository: "https://stefanprodan.github.io/podinfo/podinfo-6.9.1.tgz",
						Version:        "1.2.3", // technically version is not needed because it's a direct TGZ download
					},
				},
			},
			error: func(t *testing.T, err error) bool {
				t.Helper()
				require.NoError(t, err)

				return true
			},
		},
		{
			name: "remote chart with only version in spec",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "internal.charts.example.com/charts:6.9.1",
						HelmRepository: "oci://ghcr.io/stefanprodan/charts/podinfo",
						Version:        "6.9.1",
					},
				},
			},
			error: func(t *testing.T, err error) bool {
				t.Helper()
				require.NoError(t, err)

				return true
			},
		},
		{
			name: "neither version nor reference version specified should fail",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "internal.charts.example.com/charts",
						HelmRepository: "oci://ghcr.io/stefanprodan/charts/podinfo",
						Version:        "",
					},
				},
			},
			error: func(t *testing.T, err error) bool {
				t.Helper()
				require.Error(t, err)

				return false
			},
		},
		{
			name: "remote chart with versioned oci reference url",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "internal.charts.example.com/charts:6.9.1",
						HelmRepository: "oci://ghcr.io/stefanprodan/charts/podinfo:6.9.1",
						Version:        "6.9.1",
					},
				},
			},
			error: func(t *testing.T, err error) bool {
				t.Helper()
				require.NoError(t, err)

				return true
			},
		},
		{
			name: "should not be able to download chart if given version and reference version do not match",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "internal.charts.example.com/charts",
						HelmRepository: "oci://ghcr.io/stefanprodan/charts/podinfo:6.9.0",
						Version:        "6.9.1",
					},
				},
			},
			error: func(t *testing.T, err error) bool {
				t.Helper()
				require.ErrorContains(t, err, "chart reference and version mismatch: 6.9.1 is not 6.9.0")

				return false
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputMethod := &input.InputMethod{}

			result, err := inputMethod.ProcessResource(t.Context(), tc.resource, nil)
			if !tc.error(t, err) {
				return
			}

			assert.NotNil(t, result, "result should not be nil")
			assert.NotNil(t, result.ProcessedBlobData, "should have blob data for remote chart")
			assert.NotNil(t, result.ProcessedResource, "should have remote resource access info")

			// Verify the remote resource structure
			assert.Equal(t, input.HelmRepositoryType, result.ProcessedResource.Type, "resource type should be helmRepository")

			// Verify blob data is not empty by reading some content
			reader, err := result.ProcessedBlobData.ReadCloser()
			require.NoError(t, err)
			defer reader.Close()

			tempFile, err := os.CreateTemp(t.TempDir(), "test-")
			require.NoError(t, err)
			defer os.Remove(tempFile.Name())

			_, err = io.Copy(tempFile, reader)
			require.NoError(t, err)
			require.NotEmpty(t, tempFile.Name())
		})
	}
}
