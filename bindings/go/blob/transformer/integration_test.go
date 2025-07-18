package transformer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIntegrationHelmChartTransformer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name        string
		chartRef    string
		expectedErr bool
		skipReason  string
	}{
		{
			name:        "signed helm chart from ghcr.io",
			chartRef:    "ghcr.io/skarlso/test-signed-helmchart/chart/test-helm-chart:0.1.0",
			expectedErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipReason != "" {
				t.Skip(tt.skipReason)
			}

			ctx := context.Background()
			r := require.New(t)

			if os.Getenv("CI") != "" {
				t.Skip("skipping network-dependent test in CI")
			}

			chartBlob, err := fetchHelmChartFromOCI(ctx, tt.chartRef, "test-helm-chart", "0.1.0")
			if err != nil {
				if tt.expectedErr {
					t.Logf("Expected error occurred: %v", err)
					return
				}
				t.Fatalf("Failed to fetch chart %s: %v", tt.chartRef, err)
			}

			r.NotNil(chartBlob, "Chart blob should not be nil")

			transformer := NewHelmTransformer()
			result, err := transformer.TransformBlob(ctx, chartBlob, nil)

			if tt.expectedErr {
				r.Error(err, "Expected transformation to fail")
				return
			}

			r.NoError(err, "Transformation should succeed")
			r.NotNil(result, "Result should not be nil")

			if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
				mediaType, known := mediaTypeAware.MediaType()
				r.True(known, "Media type should be known")
				r.Equal("application/tar", mediaType, "Result should be tar format")
			}

			// Verify we can read the result
			reader, err := result.ReadCloser()
			r.NoError(err, "Should be able to read result")
			defer reader.Close()
			//
			//file, err := os.Create(filepath.Join("testdata", "chart.tar.gz"))
			//r.NoError(err, "Should be able to create temporary file")
			//defer file.Close()
			//_, err = io.Copy(file, reader)
			//r.NoError(err, "Should be able to write temporary file")

			// Let's just read for now that it's valid, we'll verify the structure later.
			buffer := make([]byte, 1024)
			n, err := reader.Read(buffer)
			r.NoError(err, "Should be able to read data from result")
			r.Greater(n, 0, "Should read some data")

			t.Logf("Successfully transformed Helm chart %s, read %d bytes", tt.chartRef, n)
		})
	}
}

func TestIntegrationOCIArtifactTransformer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	r := require.New(t)

	if os.Getenv("CI") != "" {
		t.Skip("skipping network-dependent test in CI")
	}

	chartRef := "ghcr.io/skarlso/test-signed-helmchart/chart/test-helm-chart:0.1.0"

	chartBlob, err := fetchHelmChartFromOCI(ctx, chartRef, "test-helm-chart", "0.1.0")
	r.NoError(err, "Should be able to fetch chart")
	r.NotNil(chartBlob, "Chart blob should not be nil")

	transformer := NewOCIArtifactTransformer()
	result, err := transformer.TransformBlob(ctx, chartBlob, nil)

	r.NoError(err, "Transformation should succeed")
	r.NotNil(result, "Result should not be nil")

	if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
		mediaType, known := mediaTypeAware.MediaType()
		r.True(known, "Media type should be known")
		r.Equal("application/tar", mediaType, "Result should be tar format")
	}

	reader, err := result.ReadCloser()
	r.NoError(err, "Should be able to read result")
	defer reader.Close()

	// we'll validate here that there is no provenance data and it's a different layout.
	buffer := make([]byte, 1024)
	n, err := reader.Read(buffer)
	r.NoError(err, "Should be able to read data from result")
	r.Greater(n, 0, "Should read some data")

	t.Logf("Successfully transformed OCI artifact %s using generic transformer, read %d bytes", chartRef, n)
}

// fetchHelmChartFromOCI fetches a Helm chart from an OCI registry and returns it as a blob
func fetchHelmChartFromOCI(ctx context.Context, ref, name, version string) (blob.ReadOnlyBlob, error) {
	resolver, err := urlresolver.New(urlresolver.WithBaseURL("ghcr.io"))
	if err != nil {
		return nil, fmt.Errorf("failed to create resolver: %w", err)
	}

	client := &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{
			"User-Agent": []string{"ocm-test-client"},
		},
		Cache: auth.DefaultCache,
	}
	resolver.SetClient(client)

	repo, err := oci.NewRepository(oci.WithResolver(resolver))
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    name,
				Version: version,
			},
		},
		Type: "helm.chart",
		Access: &v1.OCIImage{
			Type: runtime.Type{
				Name:    "OCIImage",
				Version: "v1",
			},
			ImageReference: ref,
		},
	}

	downloadedBlob, err := repo.DownloadResource(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed to download resource: %w", err)
	}

	return downloadedBlob, nil
}
