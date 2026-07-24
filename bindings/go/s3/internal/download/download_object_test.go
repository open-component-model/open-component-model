package download

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

type fakeGetter struct {
	body        []byte
	contentType string
	gotInput    *s3.GetObjectInput
}

func (f *fakeGetter) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.gotInput = in
	out := &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.body))}
	if f.contentType != "" {
		out.ContentType = aws.String(f.contentType)
	}
	return out, nil
}

func readAll(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func TestDownload_ForwardsRequestAndReturnsBody(t *testing.T) {
	content := []byte("payload")
	fake := &fakeGetter{body: content, contentType: "text/plain"}

	b, err := Download(context.Background(), Request{
		BucketName: "bucket",
		ObjectKey:  "key",
		Version:    "v1",
	}, WithClient(fake))
	require.NoError(t, err)
	require.Equal(t, content, readAll(t, b))

	require.Equal(t, "bucket", aws.ToString(fake.gotInput.Bucket))
	require.Equal(t, "key", aws.ToString(fake.gotInput.Key))
	require.Equal(t, "v1", aws.ToString(fake.gotInput.VersionId))

	ma, ok := b.(blob.MediaTypeAware)
	require.True(t, ok)
	mt, known := ma.MediaType()
	require.True(t, known)
	require.Equal(t, "text/plain", mt)
}

func TestDownload_MediaTypeFallback(t *testing.T) {
	// Explicit request media type wins.
	b, err := Download(context.Background(), Request{BucketName: "b", ObjectKey: "k", MediaType: "application/json"},
		WithClient(&fakeGetter{body: []byte("{}"), contentType: "text/plain"}))
	require.NoError(t, err)
	ma := b.(blob.MediaTypeAware)
	mt, _ := ma.MediaType()
	require.Equal(t, "application/json", mt)

	// No request media type and no Content-Type falls back to octet-stream.
	b, err = Download(context.Background(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: []byte("x")}))
	require.NoError(t, err)
	ma = b.(blob.MediaTypeAware)
	mt, _ = ma.MediaType()
	require.Equal(t, "application/octet-stream", mt)
}

func TestDownload_MaxDownloadSizeBoundary(t *testing.T) {
	content := []byte("0123456789") // 10 bytes

	// Exactly at the limit succeeds.
	b, err := Download(context.Background(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: content}), WithMaxDownloadSize(10))
	require.NoError(t, err)
	require.Len(t, readAll(t, b), 10)

	// One byte over the limit is rejected.
	_, err = Download(context.Background(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: content}), WithMaxDownloadSize(9))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestDownload_RequiresBucketAndKey(t *testing.T) {
	_, err := Download(context.Background(), Request{ObjectKey: "k"}, WithClient(&fakeGetter{}))
	require.Error(t, err)
	_, err = Download(context.Background(), Request{BucketName: "b"}, WithClient(&fakeGetter{}))
	require.Error(t, err)
}
