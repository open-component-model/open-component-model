package repository

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
)

const verifyTestContent = "the content a digest was taken over"

type closableBlob struct {
	*inmemory.Blob
	closed bool
}

func (c *closableBlob) Close() error {
	c.closed = true
	return nil
}

func newVerifying(t *testing.T, content string, expected digest.Digest) *verifyingBlob {
	t.Helper()
	b, err := newVerifyingBlob(inmemory.New(strings.NewReader(content)), expected)
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

	b, err := newVerifyingBlob(closable, digest.FromString(verifyTestContent))
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
		_, err := newVerifyingBlob(inmemory.New(strings.NewReader(verifyTestContent)), expected)
		require.Error(t, err, "expected digest %q must be rejected", expected)
	}
}

func TestVerifyingBlob_CopyReportsMismatch(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	err := blob.Copy(io.Discard, b)
	require.ErrorContains(t, err, "digest mismatch")
}

func TestVerifyingBlob_CopyBlobToOSPathReportsMismatch(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	err := filesystem.CopyBlobToOSPath(b, filepath.Join(t.TempDir(), "out"))
	require.ErrorContains(t, err, "digest mismatch")
}

func TestVerifyingBlob_VerifiesContentOfUnknownSize(t *testing.T) {
	b, err := newVerifyingBlob(plainBlob{content: verifyTestContent}, digest.FromString(verifyTestContent))
	require.NoError(t, err)
	require.Equal(t, blob.SizeUnknown, b.Size())

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	_, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

func TestVerifyingBlob_RejectsTrailingContent(t *testing.T) {
	b, err := newVerifyingBlob(
		sizedBlob{plainBlob{content: verifyTestContent + " and more"}, int64(len(verifyTestContent))},
		digest.FromString(verifyTestContent),
	)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	_, err = io.ReadAll(rc)
	require.ErrorContains(t, err, "digest mismatch")
	require.ErrorContains(t, rc.Close(), "digest mismatch")
}

func TestVerifyingBlob_UnreadContentCannotMatchEmptyDigest(t *testing.T) {
	b, err := newVerifyingBlob(inmemory.New(strings.NewReader(verifyTestContent)), digest.FromString(""))
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	require.ErrorContains(t, rc.Close(), "incomplete read for digest")
}

func TestVerifyingBlob_CopyOfKnownSizeVerifies(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))
	require.Equal(t, int64(len(verifyTestContent)), b.Size())

	var buf strings.Builder
	require.NoError(t, blob.Copy(&buf, b), "a size-bounded copy must not be mistaken for a partial read")
	require.Equal(t, verifyTestContent, buf.String())
}

func TestVerifyingBlob_ExactSizeReadVerifies(t *testing.T) {
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	rc, err := b.ReadCloser()
	require.NoError(t, err)

	// io.CopyN stops at the declared size, so the base never reports EOF.
	_, err = io.CopyN(io.Discard, rc, int64(len(verifyTestContent)))
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

func TestVerifyingBlob_EmptyContentVerifies(t *testing.T) {
	b, err := newVerifyingBlob(sizedBlob{plainBlob{content: ""}, 0}, digest.FromString(""))
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	require.NoError(t, rc.Close(), "a zero byte blob is complete without a single read")
	require.NoError(t, blob.Copy(io.Discard, b))
}

func TestVerifyingBlob_ContentShorterThanDeclaredSizeFails(t *testing.T) {
	b, err := newVerifyingBlob(
		sizedBlob{plainBlob{content: verifyTestContent[:10]}, int64(len(verifyTestContent))},
		digest.FromString(verifyTestContent),
	)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	_, err = io.ReadAll(rc)
	require.ErrorContains(t, err, "digest mismatch")
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
