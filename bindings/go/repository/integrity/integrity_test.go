package integrity

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const content = "the content a digest was taken over"

// sourceBlob counts how often it is opened and records whether it was closed.
type sourceBlob struct {
	content   string
	mediaType string
	opened    int
	closed    bool
}

func (s *sourceBlob) ReadCloser() (io.ReadCloser, error) {
	s.opened++
	return io.NopCloser(strings.NewReader(s.content)), nil
}

func (s *sourceBlob) MediaType() (string, bool) { return s.mediaType, s.mediaType != "" }

func (s *sourceBlob) Close() error {
	s.closed = true
	return nil
}

// fakeArtifactVerifier passes content through and records the root it verifies against.
type fakeArtifactVerifier struct {
	accepts bool
	root    digest.Digest
}

func (f *fakeArtifactVerifier) Accepts(string) bool { return f.accepts }

func (f *fakeArtifactVerifier) Verify(rc io.ReadCloser, root digest.Digest) io.ReadCloser {
	f.root = root
	return rc
}

func withNormalisation(d digest.Digest, normalisation string) *descriptor.Digest {
	hash := map[digest.Algorithm]string{digest.SHA256: "SHA-256", digest.SHA512: "SHA-512"}[d.Algorithm()]
	return &descriptor.Digest{HashAlgorithm: hash, NormalisationAlgorithm: normalisation, Value: d.Encoded()}
}

func generic(d digest.Digest) *descriptor.Digest {
	return withNormalisation(d, descriptor.GenericBlobDigestV1)
}

func readAll(t *testing.T, b blob.ReadOnlyBlob) (string, error) {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	data, readErr := io.ReadAll(rc)
	return string(data), errors.Join(readErr, rc.Close())
}

// sizedSourceBlob is a sourceBlob that knows its size, like a lazy HTTP download that
// learns it from a request, and counts how often it is asked for it.
type sizedSourceBlob struct {
	sourceBlob
	askedSize int
}

func (s *sizedSourceBlob) Size() int64 {
	s.askedSize++
	return int64(len(s.content))
}

func TestVerify_DoesNotAskLazySourceForSizeBeforeReading(t *testing.T) {
	r := require.New(t)
	source := &sizedSourceBlob{sourceBlob: sourceBlob{content: content}}

	verifying, err := Verify(t.Context(), generic(digest.FromString(content)), source)
	r.NoError(err)
	rc, err := verifying.ReadCloser()
	r.NoError(err)
	r.Zero(source.askedSize, "a lazy source must not be asked for its size before content is read")

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	r.Equal(content, string(data))
}

func TestVerify_WithoutDigest(t *testing.T) {
	for name, expected := range map[string]*descriptor.Digest{
		"nil":                     nil,
		"empty":                   {},
		"no digest":               {HashAlgorithm: descriptor.NoDigest},
		"excluded from signature": descriptor.NewExcludeFromSignatureDigest(),
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			src := &sourceBlob{content: content}

			got, err := Verify(t.Context(), expected, src)
			r.NoError(err)
			r.Same(src, got, "content without a digest must be handed back untouched")

			r.NoError(VerifyContent(t.Context(), expected, src))
			r.NoError(VerifyOCIArtifact(t.Context(), expected, digest.FromString("any")))
			r.Zero(src.opened)
		})
	}
}

func TestVerify_RefusesUnusableDigest(t *testing.T) {
	value := digest.FromString(content).Encoded()
	for name, expected := range map[string]*descriptor.Digest{
		"incomplete":                {HashAlgorithm: "SHA-256"},
		"unsupported hash":          {HashAlgorithm: "MD5", Value: value},
		"not hex":                   {HashAlgorithm: "SHA-256", Value: "not-hex"},
		"prefix of other algorithm": {HashAlgorithm: "SHA-256", Value: "sha512:" + value},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			src := &sourceBlob{content: content}

			_, err := Verify(t.Context(), expected, src)
			r.Error(err)
			r.True(src.closed, "refused content must be released")
		})
	}

	t.Run("unsupported normalisation", func(t *testing.T) {
		r := require.New(t)
		src := &sourceBlob{content: content}

		_, err := Verify(t.Context(), withNormalisation(digest.FromString(content), "jsonNormalisation/v1"), src)
		r.ErrorIs(err, ErrUnverifiable)
		r.ErrorContains(err, `unsupported normalisation algorithm "jsonNormalisation/v1"`)
		r.True(src.closed)
	})
}

func TestVerify_GenericBlobDigest(t *testing.T) {
	expected := digest.FromString(content)

	t.Run("reports the expected digest without reading", func(t *testing.T) {
		r := require.New(t)
		src := &sourceBlob{content: "something else"}

		got, err := Verify(t.Context(), generic(expected), src)
		r.NoError(err)
		dig, known := got.(blob.DigestAware).Digest()
		r.True(known)
		r.Equal(expected.String(), dig)
		r.Zero(src.opened)
	})

	t.Run("serves matching content", func(t *testing.T) {
		r := require.New(t)
		got, err := Verify(t.Context(), generic(expected), &sourceBlob{content: content})
		r.NoError(err)

		data, err := readAll(t, got)
		r.NoError(err)
		r.Equal(content, data)
	})

	t.Run("fails reading tampered content", func(t *testing.T) {
		r := require.New(t)
		got, err := Verify(t.Context(), generic(expected), &sourceBlob{content: "tampered"})
		r.NoError(err)

		rc, err := got.ReadCloser()
		r.NoError(err)
		_, err = io.ReadAll(rc)
		r.ErrorContains(err, "digest mismatch")
		r.ErrorContains(rc.Close(), "digest mismatch")
	})

	t.Run("fails closing a partial read", func(t *testing.T) {
		r := require.New(t)
		got, err := Verify(t.Context(), generic(expected), &sourceBlob{content: content})
		r.NoError(err)

		rc, err := got.ReadCloser()
		r.NoError(err)
		_, err = rc.Read(make([]byte, 4))
		r.NoError(err)
		r.ErrorContains(rc.Close(), "incomplete read")
	})

	t.Run("forwards close to the content", func(t *testing.T) {
		r := require.New(t)
		src := &sourceBlob{content: content}
		got, err := Verify(t.Context(), generic(expected), src)
		r.NoError(err)

		r.NoError(got.(io.Closer).Close())
		r.True(src.closed)
	})
}

func TestVerify_ReusesVerifiedBlob(t *testing.T) {
	expected := digest.FromString(content)

	t.Run("of the same digest", func(t *testing.T) {
		r := require.New(t)
		v := verified.New(&sourceBlob{content: content}, verified.Metadata{Digest: expected, Size: blob.SizeUnknown})

		got, err := Verify(t.Context(), generic(expected), v)
		r.NoError(err)
		r.Same(v, got, "a blob that already verifies against the digest must not be hashed twice")
	})

	t.Run("of another digest of the same algorithm", func(t *testing.T) {
		r := require.New(t)
		src := &sourceBlob{content: content}
		v := verified.New(src, verified.Metadata{Digest: digest.FromString("other"), Size: blob.SizeUnknown})

		_, err := Verify(t.Context(), generic(expected), v)
		r.ErrorContains(err, "digest mismatch")
		r.Zero(src.opened, "two digests of one algorithm must be compared without reading")
	})

	t.Run("of another algorithm", func(t *testing.T) {
		r := require.New(t)
		v := verified.New(&sourceBlob{content: "tampered"}, verified.Metadata{Digest: digest.FromString("tampered"), Size: blob.SizeUnknown})

		got, err := Verify(t.Context(), generic(digest.SHA512.FromString(content)), v)
		r.NoError(err)
		_, err = readAll(t, got)
		r.ErrorContains(err, "digest mismatch")
	})
}

func TestVerifyPrecomputed(t *testing.T) {
	t.Run("trusts a matching computed digest", func(t *testing.T) {
		r := require.New(t)
		expected := digest.FromString("y")

		got, err := VerifyPrecomputed(t.Context(), generic(expected), expected, &sourceBlob{content: "x"})
		r.NoError(err)
		data, err := readAll(t, got)
		r.NoError(err, "content hashed while it was written must not be hashed again")
		r.Equal("x", data)
		dig, _ := got.(blob.DigestAware).Digest()
		r.Equal(expected.String(), dig)
	})

	t.Run("refuses a mismatching computed digest", func(t *testing.T) {
		r := require.New(t)
		src := &sourceBlob{content: content}

		_, err := VerifyPrecomputed(t.Context(), generic(digest.FromString(content)), digest.FromString("other"), src)
		r.ErrorContains(err, "digest mismatch")
		r.True(src.closed)
	})

	t.Run("verifies on read if the computed digest is of another algorithm", func(t *testing.T) {
		r := require.New(t)
		got, err := VerifyPrecomputed(t.Context(), generic(digest.SHA512.FromString(content)),
			digest.FromString("tampered"), &sourceBlob{content: "tampered"})
		r.NoError(err)
		_, err = readAll(t, got)
		r.ErrorContains(err, "digest mismatch")
	})
}

func TestVerify_OCIArtifactDigest(t *testing.T) {
	root := digest.FromString("manifest")
	expected := withNormalisation(root, descriptor.OCIArtifactDigestV1)

	t.Run("is unverifiable without an artifact verifier", func(t *testing.T) {
		r := require.New(t)
		src := &sourceBlob{content: content, mediaType: "application/vnd.ocm.software.oci.layout.v1+tar"}

		_, err := Verify(t.Context(), expected, src)
		r.ErrorIs(err, ErrUnverifiable)
		r.True(src.closed)
	})

	t.Run("is unverifiable for content the verifier does not accept", func(t *testing.T) {
		r := require.New(t)
		_, err := Verify(t.Context(), expected, &sourceBlob{content: content, mediaType: "application/octet-stream"},
			WithArtifactVerifier(&fakeArtifactVerifier{}))
		r.ErrorIs(err, ErrUnverifiable)
		r.ErrorContains(err, `"application/octet-stream"`)
	})

	t.Run("reads accepted content through the verifier", func(t *testing.T) {
		r := require.New(t)
		verifier := &fakeArtifactVerifier{accepts: true}
		got, err := Verify(t.Context(), expected, &sourceBlob{content: content}, WithArtifactVerifier(verifier))
		r.NoError(err)

		data, err := readAll(t, got)
		r.NoError(err)
		r.Equal(content, data)
		r.Equal(root, verifier.root)
	})
}

func TestVerifyOCIArtifact(t *testing.T) {
	root := digest.FromString("manifest")

	tests := []struct {
		name     string
		expected *descriptor.Digest
		wantErr  string
	}{
		{name: "matching root", expected: withNormalisation(root, descriptor.OCIArtifactDigestV1)},
		{name: "matching root labelled as generic blob", expected: generic(root)},
		{
			name:     "other root",
			expected: withNormalisation(digest.FromString("other"), descriptor.OCIArtifactDigestV1),
			wantErr:  "OCI artifact digest mismatch: expected " + digest.FromString("other").String() + ", got " + root.String(),
		},
		{
			name:     "unsupported normalisation",
			expected: withNormalisation(root, "jsonNormalisation/v1"),
			wantErr:  "unsupported normalisation algorithm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			err := VerifyOCIArtifact(t.Context(), tt.expected, root)
			if tt.wantErr == "" {
				r.NoError(err)
				return
			}
			r.ErrorContains(err, tt.wantErr)
		})
	}
}

func TestVerifyContent(t *testing.T) {
	r := require.New(t)
	expected := generic(digest.FromString(content))

	src := &sourceBlob{content: content}
	r.NoError(VerifyContent(t.Context(), expected, src))
	r.False(src.closed, "content is not owned by VerifyContent")

	r.ErrorContains(VerifyContent(t.Context(), expected, &sourceBlob{content: "tampered"}), "digest mismatch")
}

func TestPrecomputeAlgorithm(t *testing.T) {
	r := require.New(t)

	alg, ok := PrecomputeAlgorithm(generic(digest.SHA512.FromString(content)))
	r.True(ok)
	r.Equal(digest.SHA512, alg)

	_, ok = PrecomputeAlgorithm(withNormalisation(digest.FromString(content), descriptor.OCIArtifactDigestV1))
	r.False(ok)

	_, ok = PrecomputeAlgorithm(nil)
	r.False(ok)
}
