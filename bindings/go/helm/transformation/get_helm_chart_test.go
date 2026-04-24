package transformation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmresource "ocm.software/open-component-model/bindings/go/helm/repository/resource"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	"ocm.software/open-component-model/bindings/go/helm/transformation"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// newChartRepoServer starts an httptest server that serves files from the given directory.
// It serves .tgz and .prov files directly by path.
// Why not use repotest?
// repotest does not work with otel 1.40 because:
// module go.opentelemetry.io/otel/sdk@latest found (v1.40.0), but does not contain package go.opentelemetry.io/otel/sdk/internal/internaltest
func newChartRepoServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv
}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	filesystemaccess.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.GetHelmChart{}, v1alpha1.GetHelmChartV1alpha1)
	return scheme
}

func TestGetHelmChart_Transform(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	t.Run("downloads chart from classic helm repository with provenance", func(t *testing.T) {
		// Start an HTTP file server with the test chart and provenance files
		srv := newChartRepoServer(t, "../testdata/provenance")

		t.Logf("Helm chart repo server at %s", srv.URL)

		t.Run("downloads chart and provenance files", func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			transform := &transformation.GetHelmChart{
				Scheme:             scheme,
				ResourceRepository: helmresource.NewResourceRepository(nil),
			}

			helmAccessData, err := json.Marshal(map[string]string{
				"helmRepository": srv.URL,
				"helmChart":      "mychart-0.1.0.tgz",
			})
			r.NoError(err)

			spec := &v1alpha1.GetHelmChart{
				Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
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

			result, err := transform.Transform(ctx, spec, nil)
			r.NoError(err)
			r.NotNil(result)

			helmOutput, ok := result.(*v1alpha1.GetHelmChart)
			r.True(ok)
			r.NotNil(helmOutput.Output)
			r.NotNil(helmOutput.Output.Resource)

			// Verify chart file was created
			chartPath := strings.TrimPrefix(helmOutput.Output.ChartFile.URI, "file://")
			assert.FileExists(t, chartPath)
			t.Cleanup(func() {
				_ = os.RemoveAll(chartPath)
			})

			// Verify the file is a valid helm chart
			chrt, err := loader.Load(chartPath)
			r.NoError(err)
			r.NotNil(chrt)

			accessor, err := chart.NewAccessor(chrt)
			r.NoError(err)
			assert.Equal(t, "mychart", accessor.Name())

			compareChartFiles(t, helmOutput)
			// we are in provenance test, so the provenance file should also be downloaded
			compareProvFiles(t, helmOutput)
		})

		t.Run("downloads chart using helmChart and helmRepository fields", func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			transform := &transformation.GetHelmChart{
				Scheme:             scheme,
				ResourceRepository: helmresource.NewResourceRepository(nil),
			}

			helmAccessData, err := json.Marshal(map[string]string{
				"helmRepository": srv.URL,
				"helmChart":      "mychart-0.1.0.tgz",
			})
			r.NoError(err)

			spec := &v1alpha1.GetHelmChart{
				Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
				ID:   "test-get-helm-chart-split",
				Spec: &v1alpha1.GetHelmChartSpec{
					Resource: &v2.Resource{
						ElementMeta: v2.ElementMeta{
							ObjectMeta: v2.ObjectMeta{
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

			result, err := transform.Transform(ctx, spec, nil)
			r.NoError(err)
			r.NotNil(result)

			helmOutput, ok := result.(*v1alpha1.GetHelmChart)
			r.True(ok)
			r.NotNil(helmOutput.Output)

			chartPath := strings.TrimPrefix(helmOutput.Output.ChartFile.URI, "file://")
			assert.FileExists(t, chartPath)
			t.Cleanup(func() {
				_ = os.RemoveAll(chartPath)
			})

			chrt, err := loader.Load(chartPath)
			r.NoError(err)

			accessor, err := chart.NewAccessor(chrt)
			r.NoError(err)
			assert.Equal(t, "mychart", accessor.Name())

			// Verify output resource metadata
			assert.Equal(t, "mychart", helmOutput.Output.Resource.Name)
			assert.Equal(t, "0.1.0", helmOutput.Output.Resource.Version)
			assert.NotNil(t, helmOutput.Output.ChartFile)

			compareChartFiles(t, helmOutput)
			// we are in provenance test, so the provenance file should also be downloaded
			compareProvFiles(t, helmOutput)
		})

		t.Run("downloads chart to specified output path", func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			outputDir := t.TempDir()

			transform := &transformation.GetHelmChart{
				Scheme:             scheme,
				ResourceRepository: helmresource.NewResourceRepository(nil),
			}

			helmAccessData, err := json.Marshal(map[string]string{
				"helmRepository": srv.URL,
				"helmChart":      "mychart-0.1.0.tgz",
			})
			r.NoError(err)

			spec := &v1alpha1.GetHelmChart{
				Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
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

			result, err := transform.Transform(ctx, spec, nil)
			r.NoError(err)
			r.NotNil(result)

			helmOutput, ok := result.(*v1alpha1.GetHelmChart)
			r.True(ok)
			r.NotNil(helmOutput.Output)

			// Verify chart was saved under the specified output directory
			chartPath := strings.TrimPrefix(helmOutput.Output.ChartFile.URI, "file://")
			assert.FileExists(t, chartPath)
			assert.True(t, strings.HasPrefix(chartPath, outputDir))
		})
	})

	t.Run("downloads chart from classic helm repository without provenance", func(t *testing.T) {
		// Start an HTTP file server with the test chart (no provenance files)
		srv := newChartRepoServer(t, "../testdata")

		t.Logf("Helm chart repo server at %s", srv.URL)

		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.GetHelmChart{
			Scheme:             scheme,
			ResourceRepository: helmresource.NewResourceRepository(nil),
		}

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": srv.URL,
			"helmChart":      "mychart-0.1.0.tgz",
		})
		r.NoError(err)

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
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

		result, err := transform.Transform(ctx, spec, nil)
		r.NoError(err)
		r.NotNil(result)

		helmOutput, ok := result.(*v1alpha1.GetHelmChart)
		r.True(ok)
		r.NotNil(helmOutput.Output)
		r.NotNil(helmOutput.Output.Resource)

		// Verify chart file was created
		chartPath := strings.TrimPrefix(helmOutput.Output.ChartFile.URI, "file://")
		assert.FileExists(t, chartPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(chartPath)
		})

		// Verify the file is a valid helm chart
		chrt, err := loader.Load(chartPath)
		r.NoError(err)
		r.NotNil(chrt)

		accessor, err := chart.NewAccessor(chrt)
		r.NoError(err)
		assert.Equal(t, "mychart", accessor.Name())

		compareChartFiles(t, helmOutput)
		r.Nil(helmOutput.Output.ProvFile)
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.GetHelmChart{
			Scheme:             scheme,
			ResourceRepository: helmresource.NewResourceRepository(nil),
		}

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-nil-spec",
			Spec: nil,
		}

		result, err := transform.Transform(ctx, spec, nil)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "spec is required")
	})
}

// mockResourceRepository is a test double that satisfies repository.ResourceRepository
// but only implements GetResourceCredentialConsumerIdentity with real logic.
type mockResourceRepository struct {
	identity runtime.Identity
}

func (m *mockResourceRepository) GetResourceCredentialConsumerIdentity(_ context.Context, _ *descriptor.Resource) (runtime.Identity, error) {
	return m.identity, nil
}

func (m *mockResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ map[string]string) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockResourceRepository) DownloadResource(_ context.Context, _ *descriptor.Resource, _ map[string]string) (blob.ReadOnlyBlob, error) {
	return nil, fmt.Errorf("not implemented")
}

// newAuthChartRepoServer starts an httptest server that requires Basic Auth and serves files from dir.
func newAuthChartRepoServer(t *testing.T, dir, username, password string) *httptest.Server {
	t.Helper()
	fs := http.FileServer(http.Dir(dir))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fs.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetHelmChart_CredentialFlow(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	t.Run("GetCredentialConsumerIdentities returns resource credential key", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		identity := runtime.Identity{"type": "helmChartRepository", "hostname": "charts.example.com"}

		transform := &transformation.GetHelmChart{
			Scheme:                           scheme,
			ResourceRepository: &mockResourceRepository{identity: identity},
		}

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": "https://charts.example.com/mychart-0.1.0.tgz",
		})
		r.NoError(err)

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-creds-identity",
			Spec: &v1alpha1.GetHelmChartSpec{
				Resource: &v2.Resource{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{Name: "helm", Version: "v1"},
						Data: helmAccessData,
					},
				},
			},
		}

		identities, err := transform.GetCredentialConsumerIdentities(ctx, spec)
		r.NoError(err)
		r.NotNil(identities)
		r.Contains(identities, "resource")
		assert.Equal(t, identity, identities["resource"])
	})

	t.Run("GetCredentialConsumerIdentities returns nil for nil resource", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.GetHelmChart{
			Scheme:             scheme,
			ResourceRepository: &mockResourceRepository{identity: runtime.Identity{"type": "test"}},
		}

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-nil-resource",
			Spec: &v1alpha1.GetHelmChartSpec{},
		}

		identities, err := transform.GetCredentialConsumerIdentities(ctx, spec)
		r.NoError(err)
		r.Nil(identities)
	})

	t.Run("GetCredentialConsumerIdentities returns nil for nil provider", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": "https://charts.example.com/mychart-0.1.0.tgz",
		})
		r.NoError(err)

		transform := &transformation.GetHelmChart{
			Scheme: scheme,
		}

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-nil-provider",
			Spec: &v1alpha1.GetHelmChartSpec{
				Resource: &v2.Resource{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{Name: "helm", Version: "v1"},
						Data: helmAccessData,
					},
				},
			},
		}

		identities, err := transform.GetCredentialConsumerIdentities(ctx, spec)
		r.NoError(err)
		r.Nil(identities)
	})

	// The following two subtests verify credentials survive through the helm download stack
	// (transform → downloader → HTTP). Other credential tests use mocks; these are the only
	// tests that prove auth actually reaches the wire.
	t.Run("Transform uses resolved credentials for authenticated chart download", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		srv := newAuthChartRepoServer(t, "../testdata/provenance", "helm-user", "helm-pass")

		transform := &transformation.GetHelmChart{
			Scheme:             scheme,
			ResourceRepository: helmresource.NewResourceRepository(nil),
		}

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": srv.URL,
			"helmChart":      "mychart-0.1.0.tgz",
		})
		r.NoError(err)

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-creds-transform",
			Spec: &v1alpha1.GetHelmChartSpec{
				Resource: &v2.Resource{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{Name: "helm", Version: "v1"},
						Data: helmAccessData,
					},
				},
			},
		}

		resolvedCreds := map[string]map[string]string{
			"resource": {"username": "helm-user", "password": "helm-pass"},
		}

		result, err := transform.Transform(ctx, spec, resolvedCreds)
		r.NoError(err)
		r.NotNil(result)

		helmOutput, ok := result.(*v1alpha1.GetHelmChart)
		r.True(ok)
		r.NotNil(helmOutput.Output)

		// Verify chart file was created
		chartPath := strings.TrimPrefix(helmOutput.Output.ChartFile.URI, "file://")
		assert.FileExists(t, chartPath)
		t.Cleanup(func() {
			_ = os.RemoveAll(chartPath)
		})
	})

	t.Run("Transform fails without credentials on authenticated server", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		srv := newAuthChartRepoServer(t, "../testdata/provenance", "helm-user", "helm-pass")

		transform := &transformation.GetHelmChart{
			Scheme:             scheme,
			ResourceRepository: helmresource.NewResourceRepository(nil),
		}

		helmAccessData, err := json.Marshal(map[string]string{
			"helmRepository": fmt.Sprintf("%s/mychart-0.1.0.tgz", srv.URL),
		})
		r.NoError(err)

		spec := &v1alpha1.GetHelmChart{
			Type: runtime.NewVersionedType(v1alpha1.GetHelmChartType, v1alpha1.Version),
			ID:   "test-no-creds-transform",
			Spec: &v1alpha1.GetHelmChartSpec{
				Resource: &v2.Resource{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "mychart",
							Version: "0.1.0",
						},
					},
					Type: "helmChart",
					Access: &runtime.Raw{
						Type: runtime.Type{Name: "helm", Version: "v1"},
						Data: helmAccessData,
					},
				},
			},
		}

		result, err := transform.Transform(ctx, spec, nil)
		r.Error(err)
		r.Nil(result)
	})
}

func compareFiles(t *testing.T, original string, downloaded string) {
	r := require.New(t)

	// read original file
	originalData, err := os.ReadFile(original)
	r.NoError(err)

	// read downloaded file
	// strip file:// prefix from URI to get local file path
	downloaded = strings.TrimPrefix(downloaded, "file://")
	outputData, err := os.ReadFile(downloaded)
	r.NoError(err)

	// check contents are the same
	assert.Equal(t, originalData, outputData, "downloaded content should match original")
}

func compareProvFiles(t *testing.T, helmOutput *v1alpha1.GetHelmChart) {
	compareFiles(t, "../testdata/provenance/mychart-0.1.0.tgz.prov", helmOutput.Output.ProvFile.URI)
}

func compareChartFiles(t *testing.T, helmOutput *v1alpha1.GetHelmChart) {
	compareFiles(t, "../testdata/provenance/mychart-0.1.0.tgz", helmOutput.Output.ChartFile.URI)
}
