package resource

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmdigest "ocm.software/open-component-model/bindings/go/helm/digest"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func helmResource(t *testing.T, helmRepository, helmChart string) *descriptor.Resource {
	t.Helper()

	data := map[string]string{
		"helmRepository": helmRepository,
	}
	if helmChart != "" {
		data["helmChart"] = helmChart
	}
	raw, err := json.Marshal(data)
	require.NoError(t, err)

	return &descriptor.Resource{
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("helm", "v1"),
			Data: raw,
		},
	}
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	repo := NewResourceRepository(nil)
	scheme := repo.GetResourceRepositoryScheme()
	assert.Equal(t, helmaccess.Scheme, scheme)
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	repo := NewResourceRepository(nil)
	ctx := context.Background()

	t.Run("returns identity for HTTP helm repository", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, "https://charts.example.com/stable", "")
		identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType), identity.GetType())
	})

	t.Run("returns OCI identity for OCI helm repository", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, "oci://registry.example.com/charts/mychart:1.0.0", "")
		identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, ocicredentialsspecv1.Type, identity.GetType())
	})

	t.Run("returns nil identity for empty helm repository", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, "", "")
		identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
		require.NoError(t, err)
		assert.Nil(t, identity)
	})
}

func TestConvertAccessNilGuards(t *testing.T) {
	t.Parallel()
	repo := NewResourceRepository(nil)
	ctx := context.Background()

	t.Run("returns error for nil resource", func(t *testing.T) {
		t.Parallel()
		_, err := repo.GetResourceCredentialConsumerIdentity(ctx, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error converting resource access to helm spec")
	})

	t.Run("returns error for nil access", func(t *testing.T) {
		t.Parallel()
		res := &descriptor.Resource{}
		_, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource access is required")
	})
}

func TestUploadResource(t *testing.T) {
	repo := NewResourceRepository(nil)
	res := helmResource(t, "https://charts.example.com", "mychart:1.0.0")
	_, err := repo.UploadResource(context.Background(), res, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "do not support upload")
}

func TestDownloadResource(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.FileServer(http.Dir("../../testdata")))
	t.Cleanup(srv.Close)

	repo := NewResourceRepository(nil)
	ctx := context.Background()

	t.Run("downloads chart from HTTP server", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, srv.URL, "mychart-0.1.0.tgz")
		blob, err := repo.DownloadResource(ctx, res, nil)
		require.NoError(t, err)
		require.NotNil(t, blob)

		rc, err := blob.ReadCloser()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
	})

	t.Run("downloads chart using configured temp folder", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		repoWithConfig := NewResourceRepository(&filesystemv1alpha1.Config{
			TempFolder: &tempDir,
		})

		res := helmResource(t, srv.URL, "mychart-0.1.0.tgz")
		blob, err := repoWithConfig.DownloadResource(ctx, res, nil)
		require.NoError(t, err)
		require.NotNil(t, blob)

		// The temporary download directory should have been cleaned up
		entries, err := os.ReadDir(tempDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "temp folder should be empty after download completes")

		rc, err := blob.ReadCloser()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
	})

	t.Run("returns error when helmChart is empty", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, srv.URL, "")
		_, err := repo.DownloadResource(ctx, res, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart name is required")
	})

	t.Run("returns error for empty helmRepository", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, "", "mychart-0.1.0.tgz")
		_, err := repo.DownloadResource(ctx, res, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "helm repository URL is required")
	})

	t.Run("returns error for invalid repository URL", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, "https://localhost:0/nonexistent", "mychart-0.1.0.tgz")
		_, err := repo.DownloadResource(ctx, res, nil)
		require.Error(t, err)
	})

	// Ensures that two consecutive downloads of the same chart produce a
	// byte-identical tar archive (and thus an identical digest). This guards
	// against regressions in the reproducible tar packaging, since the helm
	// download writes files with the current timestamps into a fresh temp
	// directory on each call. Without DirOptions.Reproducible those mtimes
	// would leak into the tar headers and shift the digest.
	t.Run("produces identical digest on repeated downloads", func(t *testing.T) {
		t.Parallel()
		res := helmResource(t, srv.URL, "mychart-0.1.0.tgz")

		digest1 := downloadAndDigest(t, ctx, repo, res)

		// Sleep past the 1s tar header granularity so the on-disk mtimes
		// of the second download definitely differ from the first. If the
		// reproducible flag were dropped, this delta would surface as a
		// digest mismatch.
		time.Sleep(1100 * time.Millisecond)

		digest2 := downloadAndDigest(t, ctx, repo, res)

		assert.Equal(t, digest1, digest2, "expected reproducible tar to yield the same digest across downloads")
	})
}

// downloadAndDigest downloads the resource and returns the SHA-256 digest of
// the resulting blob's bytes.
func downloadAndDigest(t *testing.T, ctx context.Context, repo *ResourceRepository, res *descriptor.Resource) string {
	t.Helper()

	b, err := repo.DownloadResource(ctx, res, nil)
	require.NoError(t, err)
	require.NotNil(t, b)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rc.Close() })

	h := sha256.New()
	_, err = io.Copy(h, rc)
	require.NoError(t, err)

	digestAware, ok := b.(blob.DigestAware)
	if !ok {
		require.Fail(t, "downloaded blob should implement DigestAware")
	}

	if d, known := digestAware.Digest(); !known || d == "" {
		require.Fail(t, "downloaded blob should have digest")
	} else {
		return hex.EncodeToString(h.Sum(nil))
	}

	return ""
}

func TestDownloadResource_VerifiesChartDigest(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir("../../testdata")))
	t.Cleanup(srv.Close)

	chart, err := os.ReadFile("../../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)

	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "matching digest", value: digest.FromBytes(chart).Encoded()},
		{name: "wrong digest", value: digest.FromString("other").Encoded(), wantErr: "does not match the digest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			res := helmResource(t, srv.URL, "mychart-0.1.0.tgz")
			res.Name = "mychart"
			res.Digest = &descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: descriptor.GenericBlobDigestV1,
				Value:                  tc.value,
			}
			b, err := NewResourceRepository(nil).DownloadResource(t.Context(), res, nil)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.NotNil(b)
		})
	}
}

func TestDownloadResource_RealOCIRepo(t *testing.T) {
	r := require.New(t)
	if testing.Short() {
		t.Skip("skipping real OCI registry test in short mode")
	}
	res := helmResource(t, "oci://ghcr.io/stefanprodan/charts", "podinfo:6.9.1")
	res.Name = "podinfo"
	processed, err := helmdigest.NewDigestProcessor("").ProcessResourceDigest(t.Context(), res, nil)
	r.NoError(err)
	r.Equal(descriptor.OCIArtifactDigestV1, processed.Digest.NormalisationAlgorithm)

	repo := NewResourceRepository(nil)
	b, err := repo.DownloadResource(t.Context(), processed, nil)
	r.NoError(err)
	rc, err := b.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	var names []string
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		r.NoError(err)
		names = append(names, hdr.Name)
	}
	r.Contains(names, "podinfo-6.9.1.tgz")

	other := processed.DeepCopy()
	other.Digest.Value = digest.FromString("other").Encoded()
	_, err = repo.DownloadResource(t.Context(), other, nil)
	r.ErrorContains(err, `error downloading chart "oci://ghcr.io/stefanprodan/charts/podinfo:6.9.1" version ""`)
	r.ErrorContains(err, "not found")
}

func TestManifestPin(t *testing.T) {
	tgz, err := os.ReadFile("../../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	srv, manifest := newChartRegistry(t, "charts/mychart", "0.1.0", tgz)
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	repo := NewResourceRepository(nil, WithHTTPConfig(&httpv1alpha1.Config{TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: caPEM}}))
	chartRef := "oci://" + strings.TrimPrefix(srv.URL, "https://") + "/charts/mychart:0.1.0"
	other := digest.FromString("other")

	tests := []struct {
		name    string
		digest  *descriptor.Digest
		want    digest.Digest
		wantErr string
	}{
		{name: "no digest", digest: nil},
		{
			name:   "ociArtifactDigest/v1 pins without resolving",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: other.Encoded()},
			want:   other,
		},
		{
			name:   "genericBlobDigest/v1 naming the manifest pins",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: manifest.Encoded()},
			want:   manifest,
		},
		{
			name:   "empty normalisation naming the manifest pins",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: manifest.Encoded()},
			want:   manifest,
		},
		{
			name:   "genericBlobDigest/v1 naming the chart archive does not pin",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: digest.FromBytes(tgz).Encoded()},
		},
		{
			name:   "other normalisation does not pin",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v1", Value: manifest.Encoded()},
		},
		{
			name:    "invalid digest fails",
			digest:  &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: "not-hex"},
			wantErr: `resource "mychart": invalid digest`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			res := &descriptor.Resource{Digest: tc.digest}
			res.Name = "mychart"
			got, err := repo.manifestPin(t.Context(), res, chartRef, nil)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}

// newChartRegistry serves tgz as a helm chart at repo:tag from a read-only OCI distribution
// endpoint over TLS and returns the server and the chart's manifest digest.
func newChartRegistry(t *testing.T, repo, tag string, tgz []byte) (*httptest.Server, digest.Digest) {
	t.Helper()
	config := []byte(`{"apiVersion":"v2","name":"mychart","version":"0.1.0"}`)
	manifest, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config: ociImageSpecV1.Descriptor{
			MediaType: registry.ConfigMediaType,
			Digest:    digest.FromBytes(config),
			Size:      int64(len(config)),
		},
		Layers: []ociImageSpecV1.Descriptor{{
			MediaType: registry.ChartLayerMediaType,
			Digest:    digest.FromBytes(tgz),
			Size:      int64(len(tgz)),
		}},
	})
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifest)

	manifests := map[string][]byte{tag: manifest, manifestDigest.String(): manifest}
	blobs := map[string][]byte{
		digest.FromBytes(config).String(): config,
		digest.FromBytes(tgz).String():    tgz,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		rest, ok := strings.CutPrefix(req.URL.Path, "/v2/"+repo+"/")
		if !ok {
			http.NotFound(w, req)
			return
		}
		var data []byte
		mediaType := "application/octet-stream"
		if ref, isManifest := strings.CutPrefix(rest, "manifests/"); isManifest {
			data, mediaType = manifests[ref], ociImageSpecV1.MediaTypeImageManifest
		} else if ref, isBlob := strings.CutPrefix(rest, "blobs/"); isBlob {
			data = blobs[ref]
		}
		if data == nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", digest.FromBytes(data).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if req.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, manifestDigest
}
