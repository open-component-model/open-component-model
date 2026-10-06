package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

const (
	rustfsImage     = "rustfs/rustfs:1.0.0-rc.6"
	rustfsAccessKey = "ocm-test"
	rustfsSecretKey = "ocm-test-secret"
)

// startRustFS starts a RustFS S3 server and returns its host:port.
func startRustFS(t *testing.T, ctx context.Context) string {
	t.Helper()
	r := require.New(t)

	container, err := testcontainers.Run(ctx, rustfsImage,
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": rustfsAccessKey, "RUSTFS_SECRET_KEY": rustfsSecretKey}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort("9000/tcp")),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	hostPort, err := container.PortEndpoint(ctx, "9000/tcp", "")
	r.NoError(err)
	return hostPort
}

// Test_Integration_S3 exercises the S3 ResourceRepository end to end against a RustFS
// container.
func Test_Integration_S3(t *testing.T) {
	ctx := context.Background()

	hostPort := startRustFS(t, ctx)
	endpoint := "http://" + hostPort

	setup := newSetupClient(t, ctx, endpoint, rustfsAccessKey, rustfsSecretKey)

	creds := &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     rustfsAccessKey,
		SecretAccessKey: rustfsSecretKey,
	}
	tempDir := t.TempDir()
	fsConfig := &filesystemv1alpha1.Config{TempFolder: &tempDir}
	// A real http configuration: the retry section drives the SDK's attempt count, and
	// the per-host entry routes S3 requests through the host-specific transport.
	httpConfig := &httpv1alpha1.Config{
		TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: httpv1alpha1.NewTimeout(30 * time.Second)},
		Retry: &httpv1alpha1.RetryConfig{
			MaxRetries: new(2),
			MinWait:    httpv1alpha1.NewTimeout(50 * time.Millisecond),
			MaxWait:    httpv1alpha1.NewTimeout(time.Second),
		},
		Hosts: map[string]*httpv1alpha1.HostConfig{
			hostPort: {TimeoutConfig: httpv1alpha1.TimeoutConfig{
				ResponseHeaderTimeout: httpv1alpha1.NewTimeout(10 * time.Second),
			}},
		},
	}
	repo := repository.NewResourceRepository(fsConfig, repository.WithHTTPConfig(httpConfig))

	access := func(bucket, key, version string) *accessv2.S3 {
		return &accessv2.S3{
			Type:         accessspec.V2VersionedType,
			Region:       "us-east-1",
			BucketName:   bucket,
			ObjectKey:    key,
			Endpoint:     endpoint,
			UsePathStyle: true,
			Version:      version,
		}
	}
	resourceFor := func(a *accessv2.S3) *descriptor.Resource {
		res := &descriptor.Resource{}
		res.Access = a
		return res
	}

	t.Run("download and digest", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "download-bucket", "path/to/blob.txt"
		content := []byte("hello ocm from s3")
		createBucket(t, ctx, setup, bucket)
		putObject(t, ctx, setup, bucket, key, content)

		res := resourceFor(access(bucket, key, ""))

		b, err := repo.DownloadResource(ctx, res, creds)
		r.NoError(err)
		rc, err := b.ReadCloser()
		r.NoError(err)
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(content, got)

		withDigest, err := repo.ProcessResourceDigest(ctx, res, creds)
		r.NoError(err)
		r.NotNil(withDigest.Digest)
		r.Equal(godigest.FromBytes(content).Encoded(), withDigest.Digest.Value)
		r.Equal("SHA-256", withDigest.Digest.HashAlgorithm)
	})

	for _, tc := range []struct {
		algorithm types.ChecksumAlgorithm
		digest    godigest.Algorithm
		name      string
	}{
		{algorithm: types.ChecksumAlgorithmSha256, digest: godigest.SHA256, name: "SHA-256"},
		{algorithm: types.ChecksumAlgorithmSha512, digest: godigest.SHA512, name: "SHA-512"},
	} {
		t.Run("digest from the store's "+tc.name+" checksum", func(t *testing.T) {
			r := require.New(t)
			bucket, key := "checksum-"+strings.ToLower(string(tc.algorithm)), "blob"
			content := []byte("digested without download")
			createBucket(t, ctx, setup, bucket)
			_, err := setup.PutObject(ctx, &s3.PutObjectInput{
				Bucket:            new(bucket),
				Key:               new(key),
				Body:              bytes.NewReader(content),
				ChecksumAlgorithm: tc.algorithm,
			})
			r.NoError(err)

			var methods []string
			client := &http.Client{Transport: recordingTransport(func(req *http.Request) (*http.Response, error) {
				methods = append(methods, req.Method)
				return http.DefaultTransport.RoundTrip(req)
			})}
			recorded := repository.NewResourceRepository(fsConfig, repository.WithHTTPClient(client))

			withDigest, err := recorded.ProcessResourceDigest(ctx, resourceFor(access(bucket, key, "")), creds)
			r.NoError(err)
			r.Equal(tc.name, withDigest.Digest.HashAlgorithm)
			r.Equal(tc.digest.FromBytes(content).Encoded(), withDigest.Digest.Value)
			r.Equal([]string{http.MethodHead}, methods, "a stored full-object checksum must make the download unnecessary")
		})
	}

	t.Run("multipart object is downloaded part by part", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "multipart-bucket", "blob"
		createBucket(t, ctx, setup, bucket)
		// S3 requires every part but the last to be at least 5 MiB.
		const partSize = 5 * 1024 * 1024
		parts := [][]byte{
			bytes.Repeat([]byte("a"), partSize),
			bytes.Repeat([]byte("b"), partSize),
			[]byte("tail"),
		}
		content := bytes.Join(parts, nil)
		upload, err := setup.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: new(bucket), Key: new(key), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		})
		r.NoError(err)
		var completed []types.CompletedPart
		for i, part := range parts {
			number := int32(i + 1)
			out, err := setup.UploadPart(ctx, &s3.UploadPartInput{
				Bucket: new(bucket), Key: new(key), UploadId: upload.UploadId, PartNumber: &number,
				Body: bytes.NewReader(part), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
			})
			r.NoError(err)
			completed = append(completed, types.CompletedPart{ETag: out.ETag, PartNumber: &number, ChecksumSHA256: out.ChecksumSHA256})
		}
		_, err = setup.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
			Bucket: new(bucket), Key: new(key), UploadId: upload.UploadId,
			MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
		})
		r.NoError(err)

		var mu sync.Mutex
		var partNumbers []string
		client := &http.Client{Transport: recordingTransport(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet {
				mu.Lock()
				partNumbers = append(partNumbers, req.URL.Query().Get("partNumber"))
				mu.Unlock()
			}
			return http.DefaultTransport.RoundTrip(req)
		})}
		recorded := repository.NewResourceRepository(fsConfig, repository.WithHTTPClient(client))

		withDigest, err := recorded.ProcessResourceDigest(ctx, resourceFor(access(bucket, key, "")), creds)
		r.NoError(err)
		r.Equal(godigest.FromBytes(content).Encoded(), withDigest.Digest.Value, "the composite checksum must not stand in for the digest")
		r.ElementsMatch([]string{"1", "2", "3"}, partNumbers, "every part is fetched on its own")

		b, err := recorded.DownloadResource(ctx, withDigest, creds)
		r.NoError(err)
		rc, err := b.ReadCloser()
		r.NoError(err)
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.True(bytes.Equal(content, got), "the parts reassemble into the object")
	})

	t.Run("pinned object version", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "versioned-bucket", "blob"
		createBucket(t, ctx, setup, bucket)
		enableVersioning(t, ctx, setup, bucket)

		v1Content := []byte("version one")
		v1ID := putObjectReturningVersion(t, ctx, setup, bucket, key, v1Content)
		putObject(t, ctx, setup, bucket, key, []byte("version two"))

		b, err := repo.DownloadResource(ctx, resourceFor(access(bucket, key, v1ID)), creds)
		r.NoError(err)
		rc, err := b.ReadCloser()
		r.NoError(err)
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(v1Content, got, "pinned versionId must return the exact original object")
	})

	t.Run("ocmv1 access compatibility", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		const bucket, key = "ocmv1-compatibility", "path/to/blob.txt"
		content := []byte("object referenced by an OCM v1 access record")
		createBucket(t, ctx, setup, bucket)
		enableVersioning(t, ctx, setup, bucket)
		version := putObjectReturningVersion(t, ctx, setup, bucket, key, content)
		r.NotEmpty(version)

		for _, tt := range []struct {
			name, typ, bucketField, keyField string
		}{
			{"explicit v1", "s3/v1", "bucket", "key"},
			{"explicit v2", "s3/v2", "bucketName", "objectKey"},
			{"unversioned legacy fields", "s3", "bucket", "key"},
			{"unversioned modern fields", "s3", "bucketName", "objectKey"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				r := require.New(t)
				// Endpoint and path style route the legacy wire format to the test store.
				data, err := json.Marshal(map[string]any{
					"type": tt.typ, tt.bucketField: bucket, tt.keyField: key,
					"region": "us-east-1", "mediaType": "text/plain",
					"endpoint": endpoint, "usePathStyle": true,
				})
				r.NoError(err)
				for _, wire := range []runtime.Typed{&runtime.Raw{}, &runtime.Unstructured{}} {
					r.NoError(json.Unmarshal(data, wire))
					res := &descriptor.Resource{}
					res.Access = wire
					before := res.DeepCopy()

					b, err := repo.DownloadResource(ctx, res, creds)
					r.NoError(err)
					reader, err := b.ReadCloser()
					r.NoError(err)
					got, err := io.ReadAll(reader)
					r.NoError(reader.Close())
					r.NoError(err)
					r.Equal(content, got)

					pinned, err := repo.ProcessResourceDigest(ctx, res, creds)
					r.NoError(err)
					r.Equal(before, res, "compatibility decoding must not modify the original access")
					r.NotNil(pinned.Digest)
					r.Equal(godigest.FromBytes(content).Encoded(), pinned.Digest.Value)
					r.Equal("SHA-256", pinned.Digest.HashAlgorithm)
					var normalized accessv2.S3
					r.NoError(accessspec.Scheme.Convert(pinned.Access, &normalized))
					r.Equal(bucket, normalized.BucketName)
					r.Equal(key, normalized.ObjectKey)
					r.Equal(version, normalized.Version)
					encoded, err := json.Marshal(pinned.Access)
					r.NoError(err)
					var fields map[string]any
					r.NoError(json.Unmarshal(encoded, &fields))
					r.NotContains(fields, "bucket")
					r.NotContains(fields, "key")
					r.Equal(bucket, fields["bucketName"])
					r.Equal(key, fields["objectKey"])
				}
			})
		}
	})

	t.Run("missing object errors", func(t *testing.T) {
		r := require.New(t)
		const bucket = "missing-bucket"
		createBucket(t, ctx, setup, bucket)

		_, err := repo.DownloadResource(ctx, resourceFor(access(bucket, "does/not/exist", "")), creds)
		r.Error(err)
	})

	t.Run("http config timeout is enforced", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "timeout-bucket", "blob"
		createBucket(t, ctx, setup, bucket)
		putObject(t, ctx, setup, bucket, key, []byte("never read"))

		// A timeout no request can meet, with retries off so the single attempt fails fast.
		strict := repository.NewResourceRepository(fsConfig, repository.WithHTTPConfig(&httpv1alpha1.Config{
			TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: httpv1alpha1.NewTimeout(time.Nanosecond)},
			Retry:         &httpv1alpha1.RetryConfig{MaxRetries: new(-1)},
		}))

		_, err := strict.DownloadResource(ctx, resourceFor(access(bucket, key, "")), creds)
		var netErr net.Error
		r.ErrorAs(err, &netErr)
		r.True(netErr.Timeout(), "download must fail on the configured timeout, got %v", err)
	})
}

func newSetupClient(t *testing.T, ctx context.Context, endpoint, user, pass string) *s3.Client {
	t.Helper()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(user, pass, "")),
	)
	require.NoError(t, err)
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = new(endpoint)
		o.UsePathStyle = true
	})
}

func createBucket(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: new(bucket)})
	require.NoError(t, err)
}

func enableVersioning(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  new(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
	})
	require.NoError(t, err)
}

type recordingTransport func(*http.Request) (*http.Response, error)

func (f recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func putObject(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, content []byte) {
	t.Helper()
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: new(bucket),
		Key:    new(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err)
}

func putObjectReturningVersion(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, content []byte) string {
	t.Helper()
	out, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: new(bucket),
		Key:    new(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err)
	require.NotNil(t, out.VersionId)
	return aws.ToString(out.VersionId)
}
