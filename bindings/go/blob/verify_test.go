package blob_test

import (
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

const verifyTestContent = "the content a digest was taken over"

// closableBlob is a blob that owns a resource, to assert that wrapping it does not
// hide the Close that releases it.
type closableBlob struct {
	*inmemory.Blob
	closed bool
}

func (c *closableBlob) Close() error {
	c.closed = true
	return nil
}

// plainBlob implements nothing beyond ReadOnlyBlob.
type plainBlob struct {
	content string
}

func (p plainBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(p.content)), nil
}

// sizedBlob gives a plainBlob a size without giving it anything else.
type sizedBlob struct {
	plainBlob
	size int64
}

func (s sizedBlob) Size() int64 { return s.size }

func newVerifying(t *testing.T, content string, expected digest.Digest) *blob.VerifyingBlob {
	t.Helper()
	b, err := blob.NewVerifyingBlob(inmemory.New(strings.NewReader(content)), expected)
	require.NoError(t, err)
	return b
}

func TestVerifyingBlob_MatchingContent(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))
	requireVerified(t, b)

	// Verify reads from the start, so it leaves the content available to the caller.
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	read, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, verifyTestContent, string(read))
}

func TestVerifyingBlob_TamperedContent(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))
	requireVerifyError(t, b, "digest mismatch")
}

func TestVerifyingBlob_VerifyIsRepeatable(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))
	for range 2 {
		requireVerified(t, b)
	}
}

// requireVerified asserts the content was checked and held up.
func requireVerified(t *testing.T, b *blob.VerifyingBlob) {
	t.Helper()
	verified, err := b.Verify(t.Context())
	require.NoError(t, err)
	require.True(t, verified)
}

// requireVerifyError asserts verification was attempted and failed.
func requireVerifyError(t *testing.T, b *blob.VerifyingBlob, contains string) {
	t.Helper()
	verified, err := b.Verify(t.Context())
	require.ErrorContains(t, err, contains)
	require.False(t, verified)
}

// Reading is deliberately not verification: the caller decides by calling Verify.
// This is the trade that lets the digest processor download without being held to
// a digest it has not computed yet.
func TestVerifyingBlob_ReadingDoesNotVerify(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	read, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, verifyTestContent, string(read))

	requireVerifyError(t, b, "digest mismatch")
}

func TestVerifyingBlob_RejectsTrailingContent(t *testing.T) {
	// The size comes from the blob, the digest from the descriptor. Content longer
	// than the declared size is cut off before it is ever hashed.
	b, err := blob.NewVerifyingBlob(
		sizedBlob{plainBlob{content: verifyTestContent + " and more"}, int64(len(verifyTestContent))},
		digest.FromString(verifyTestContent),
	)
	require.NoError(t, err)

	requireVerifyError(t, b, "digest mismatch")
	requireVerifyError(t, b, "trailing data")
}

func TestVerifyingBlob_NothingToVerify(t *testing.T) {
	inner := inmemory.New(strings.NewReader(verifyTestContent))
	b, err := blob.NewVerifyingBlob(inner, "")
	require.NoError(t, err)

	// A resource is allowed to carry no digest. Nothing was checked, and no error:
	// whether that is acceptable is the caller's policy.
	verified, err := b.Verify(t.Context())
	require.NoError(t, err)
	require.False(t, verified)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	read, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, verifyTestContent, string(read))
}

// Digest must report what the content IS, never the expectation. The github digest
// processor downloads through DownloadResource and reads this back to establish the
// digest; reporting the expectation would hand it its own input.
func TestVerifyingBlob_DigestReportsActualContent(t *testing.T) {
	promised := digest.FromString("what the descriptor promised")
	b := newVerifying(t, verifyTestContent, promised)

	dig, known := b.Digest()
	require.True(t, known)
	require.Equal(t, digest.FromString(verifyTestContent).String(), dig)
	require.NotEqual(t, promised.String(), dig)
}

func TestVerifyingBlob_ForwardsToUnderlyingBlob(t *testing.T) {
	inner := inmemory.New(strings.NewReader(verifyTestContent), inmemory.WithMediaType("application/x-tar"))
	closable := &closableBlob{Blob: inner}

	b, err := blob.NewVerifyingBlob(closable, digest.FromString(verifyTestContent))
	require.NoError(t, err)

	require.Equal(t, int64(len(verifyTestContent)), b.Size())

	mediaType, known := b.MediaType()
	require.True(t, known)
	require.Equal(t, "application/x-tar", mediaType)

	b.SetMediaType("application/octet-stream")
	mediaType, _ = b.MediaType()
	require.Equal(t, "application/octet-stream", mediaType)

	require.NoError(t, b.Close())
	require.True(t, closable.closed, "wrapping must not hide the Close that releases the underlying resource")
}

// Construction fails on a digest that cannot be used, rather than handing back a
// blob that only reports the problem once someone verifies it.
func TestVerifyingBlob_RejectsUnusableExpectedDigest(t *testing.T) {
	for _, expected := range []digest.Digest{"not-a-digest", "sha256:tooshort", "md5:abcd"} {
		_, err := blob.NewVerifyingBlob(inmemory.New(strings.NewReader(verifyTestContent)), expected)
		require.Error(t, err, "expected digest %q must be rejected", expected)
	}
}

func TestVerifyingBlob_RejectsUnknownSize(t *testing.T) {
	// VerifyReader bounds the content by the declared size, so an unknown size would
	// let it read nothing at all and call that verified.
	_, err := blob.NewVerifyingBlob(plainBlob{content: verifyTestContent}, digest.FromString(verifyTestContent))
	require.ErrorContains(t, err, "unknown size")

	_, err = blob.NewVerifyingBlob(sizedBlob{plainBlob{content: verifyTestContent}, blob.SizeUnknown},
		digest.FromString(verifyTestContent))
	require.ErrorContains(t, err, "unknown size")
}
