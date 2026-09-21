package verify_test

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/internal/verify"
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

func newVerifying(t *testing.T, content string, expected digest.Digest) *verify.Blob {
	t.Helper()
	b, err := verify.NewBlob(inmemory.New(strings.NewReader(content)), expected)
	require.NoError(t, err)
	return b
}

func TestVerifyingBlob_MatchingContent(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	rc, err := b.ReadCloser()
	require.NoError(t, err)

	read, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, verifyTestContent, string(read))
	require.NoError(t, rc.Close())

	dig, known := b.Digest()
	require.True(t, known)
	require.Equal(t, digest.FromString(verifyTestContent).String(), dig)
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

func TestVerifyingBlob_TamperedContent(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	rc, err := b.ReadCloser()
	require.NoError(t, err)

	// VerifyReader itself only checks in Verify, so this asserts the reader drives
	// it at EOF: a caller that never inspects the error from Close still cannot use
	// the content unknowingly.
	_, err = io.ReadAll(rc)
	require.ErrorContains(t, err, "digest mismatch")
	require.ErrorContains(t, rc.Close(), "digest mismatch")
}

func TestVerifyingBlob_PartialReadFailsOnClose(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	rc, err := b.ReadCloser()
	require.NoError(t, err)

	_, err = io.CopyN(io.Discard, rc, 4)
	require.NoError(t, err)

	// A prefix cannot be held to a digest over the whole, so it must not pass.
	require.ErrorContains(t, rc.Close(), "digest mismatch")
}

func TestVerifyingBlob_EachReaderVerifiesIndependently(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	for range 2 {
		rc, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
	}
}

func TestVerifyingBlob_ForwardsToUnderlyingBlob(t *testing.T) {
	inner := inmemory.New(strings.NewReader(verifyTestContent), inmemory.WithMediaType("application/x-tar"))
	closable := &closableBlob{Blob: inner}

	b, err := verify.NewBlob(closable, digest.FromString(verifyTestContent))
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

func TestVerifyingBlob_RejectsUnusableExpectedDigest(t *testing.T) {
	for _, expected := range []digest.Digest{"", "not-a-digest", "sha256:tooshort"} {
		_, err := verify.NewBlob(inmemory.New(strings.NewReader(verifyTestContent)), expected)
		require.Error(t, err, "expected digest %q must be rejected", expected)
	}
}

func TestVerifyingBlob_CopyReportsMismatch(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	// Copy picks the expected digest up through DigestAware, so the check holds
	// even for callers that never touch the reader themselves.
	err := blob.Copy(io.Discard, b)
	require.ErrorContains(t, err, "digest mismatch")
}

func TestVerifyingBlob_CopyBlobToOSPathReportsMismatch(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	err := filesystem.CopyBlobToOSPath(b, filepath.Join(t.TempDir(), "out"))
	require.ErrorContains(t, err, "digest mismatch")
}

func TestVerifyingBlob_RejectsUnknownSize(t *testing.T) {
	// VerifyReader bounds the content by the declared size, so an unknown size would
	// let it read nothing at all and call that verified.
	_, err := verify.NewBlob(plainBlob{content: verifyTestContent}, digest.FromString(verifyTestContent))
	require.ErrorContains(t, err, "unknown size")

	_, err = verify.NewBlob(sizedBlob{plainBlob{content: verifyTestContent}, blob.SizeUnknown}, digest.FromString(verifyTestContent))
	require.ErrorContains(t, err, "unknown size")
}

func TestVerifyingBlob_RejectsTrailingContent(t *testing.T) {
	// The size comes from the blob, the digest from the descriptor. Content longer
	// than the size is cut off by VerifyReader before it is ever hashed.
	b, err := verify.NewBlob(
		sizedBlob{plainBlob{content: verifyTestContent + " and more"}, int64(len(verifyTestContent))},
		digest.FromString(verifyTestContent),
	)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	// Caught at the size boundary, before the extra bytes are read. It surfaces as
	// a mismatch like any other, with what the verifier actually found kept as detail.
	_, err = io.ReadAll(rc)
	require.ErrorContains(t, err, "digest mismatch")
	require.ErrorContains(t, err, "trailing data")
	require.ErrorContains(t, rc.Close(), "digest mismatch")
}

// sizedBlob gives a plainBlob a size without giving it anything else.
type sizedBlob struct {
	plainBlob
	size int64
}

func (s sizedBlob) Size() int64 { return s.size }

// plainBlob implements nothing beyond ReadOnlyBlob.
type plainBlob struct {
	content string
}

func (p plainBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(p.content)), nil
}
