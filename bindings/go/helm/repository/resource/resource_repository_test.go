package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
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
		res := helmResource(t, "https://charts.example.com/stable", "")
		identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType), identity.GetType())
	})

	t.Run("returns OCI identity for OCI helm repository", func(t *testing.T) {
		res := helmResource(t, "oci://registry.example.com/charts/mychart:1.0.0", "")
		identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, ocicredentialsspecv1.Type, identity.GetType())
	})

	t.Run("returns nil identity for empty helm repository", func(t *testing.T) {
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
		_, err := repo.GetResourceCredentialConsumerIdentity(ctx, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error converting resource access to helm spec")
	})

	t.Run("returns error for nil access", func(t *testing.T) {
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
		res := helmResource(t, srv.URL, "mychart-0.1.0.tgz")
		blob, err := repo.DownloadResource(ctx, res, nil)
		require.NoError(t, err)
		require.NotNil(t, blob)

		rc, err := blob.ReadCloser()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
	})

	t.Run("downloads chart using configured temp folder", func(t *testing.T) {
		tempDir := t.TempDir()
		repoWithConfig := NewResourceRepository(&filesystemv1alpha1.Config{
			TempFolder: tempDir,
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
		res := helmResource(t, srv.URL, "")
		_, err := repo.DownloadResource(ctx, res, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart name is required")
	})

	t.Run("returns error for empty helmRepository", func(t *testing.T) {
		res := helmResource(t, "", "mychart-0.1.0.tgz")
		_, err := repo.DownloadResource(ctx, res, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "helm repository URL is required")
	})

	t.Run("returns error for invalid repository URL", func(t *testing.T) {
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
