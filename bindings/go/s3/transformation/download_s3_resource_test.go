package transformation_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	s3access "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/s3/transformation"
	"ocm.software/open-component-model/bindings/go/s3/transformation/spec/v1alpha1"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	filesystemaccess.MustAddToScheme(scheme)
	s3access.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.DownloadS3Resource{}, v1alpha1.DownloadS3ResourceV1alpha1)
	return scheme
}

// staticCredentials resolves the same S3 credentials for every identity. The fake S3 server
// does not verify the signature, but credentials must be supplied so that the SDK signs with
// them instead of falling back to the ambient credential chain of the machine running the test.
type staticCredentials struct{}

func (staticCredentials) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	return &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}, nil
}

// newFakeS3 starts a server answering GetObject with body, so that the transformer is
// exercised over the real AWS SDK rather than a stubbed client.
func newFakeS3(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Real S3 reports a checksum, and the SDK warns about every response carrying none.
		sum := make([]byte, 4)
		binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(body))
		w.Header().Set("x-amz-checksum-crc32", base64.StdEncoding.EncodeToString(sum))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func s3Resource(t *testing.T, endpoint string) *v2.Resource {
	t.Helper()
	access := &s3v1.S3Bucket{
		Type:         s3access.V1VersionedType,
		Region:       "us-east-1",
		BucketName:   "my-bucket",
		ObjectKey:    "path/to/myfile",
		Endpoint:     endpoint,
		UsePathStyle: true,
	}
	raw := &runtime.Raw{}
	require.NoError(t, s3access.Scheme.Convert(access, raw))
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{
				Name:    "myfile",
				Version: "1.0.0",
			},
		},
		Type:   "blob",
		Access: raw,
	}
}

func TestDownloadS3Resource_Transform(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	const content = "hello s3 transfer"

	srv := newFakeS3(t, []byte(content))

	t.Run("downloads resource to a file", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.DownloadS3Resource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
			CredentialProvider: staticCredentials{},
		}

		spec := &v1alpha1.DownloadS3Resource{
			Type: v1alpha1.DownloadS3ResourceV1alpha1,
			ID:   "test-get-s3",
			Spec: &v1alpha1.DownloadS3ResourceSpec{
				Resource: s3Resource(t, srv.URL),
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		out, ok := result.(*v1alpha1.DownloadS3Resource)
		r.True(ok)
		r.NotNil(out.Output)
		r.NotNil(out.Output.Resource)

		filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
		assert.FileExists(t, filePath)
		t.Cleanup(func() { _ = os.RemoveAll(filePath) })

		data, err := os.ReadFile(filePath)
		r.NoError(err)
		assert.Equal(t, content, string(data), "downloaded content should match the object served")

		assert.Equal(t, "myfile", out.Output.Resource.Name)
		assert.Equal(t, "1.0.0", out.Output.Resource.Version)
	})

	t.Run("downloads to specified output directory", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		transform := &transformation.DownloadS3Resource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
			CredentialProvider: staticCredentials{},
		}

		spec := &v1alpha1.DownloadS3Resource{
			Type: v1alpha1.DownloadS3ResourceV1alpha1,
			ID:   "test-get-s3-output-path",
			Spec: &v1alpha1.DownloadS3ResourceSpec{
				Resource:   s3Resource(t, srv.URL),
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)

		out, ok := result.(*v1alpha1.DownloadS3Resource)
		r.True(ok)
		r.NotNil(out.Output)

		filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
		assert.FileExists(t, filePath)
		assert.True(t, strings.HasPrefix(filePath, outputDir))
	})

	t.Run("removes the output file when the download fails", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		t.Cleanup(failSrv.Close)

		transform := &transformation.DownloadS3Resource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
			CredentialProvider: staticCredentials{},
		}

		spec := &v1alpha1.DownloadS3Resource{
			Type: v1alpha1.DownloadS3ResourceV1alpha1,
			ID:   "test-cleanup-on-failure",
			Spec: &v1alpha1.DownloadS3ResourceSpec{
				Resource:   s3Resource(t, failSrv.URL),
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)

		entries, err := os.ReadDir(outputDir)
		r.NoError(err)
		assert.Empty(t, entries, "temporary output file should be removed after a failed download")
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.DownloadS3Resource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
			CredentialProvider: staticCredentials{},
		}

		spec := &v1alpha1.DownloadS3Resource{
			Type: v1alpha1.DownloadS3ResourceV1alpha1,
			ID:   "test-nil-spec",
			Spec: nil,
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "spec is required")
	})

	t.Run("fails when resource is nil", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.DownloadS3Resource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
			CredentialProvider: staticCredentials{},
		}

		spec := &v1alpha1.DownloadS3Resource{
			Type: v1alpha1.DownloadS3ResourceV1alpha1,
			ID:   "test-nil-resource",
			Spec: &v1alpha1.DownloadS3ResourceSpec{
				Resource: nil,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "resource is required")
	})
}
