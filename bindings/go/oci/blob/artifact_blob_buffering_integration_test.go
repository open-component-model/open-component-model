package blob_test

import (
	"bytes"
	_ "crypto/sha512" // Exercise byte hints with a different algorithm from the resource digest.
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
)

func Test_Integration_ArtifactBlob_BufferExpectedChecksum(t *testing.T) {
	for _, normalization := range []string{internaldigest.GenericBlobDigestV1, ""} {
		for _, tt := range []struct {
			name      string
			mutate    bool
			override  bool
			algorithm digest.Algorithm
		}{
			{name: "unchanged_file"},
			{name: "same_size_replacement", mutate: true},
			{name: "same_size_replacement_with_precalculated_digest", mutate: true, override: true},
			{name: "unchanged_file_with_sha512_hint", override: true, algorithm: digest.SHA512},
			{name: "same_size_replacement_with_sha512_hint", mutate: true, override: true, algorithm: digest.SHA512},
		} {
			t.Run(normalization+"/"+tt.name, func(t *testing.T) {
				r := require.New(t)
				original, replacement := []byte("content A"), []byte("content B")
				r.Len(replacement, len(original))
				ab, path, _ := newArtifactBlobBufferingFile(t, original, normalization)
				r.Equal(int64(len(original)), ab.Size())
				if tt.mutate {
					r.NoError(os.WriteFile(path, replacement, 0o600))
					r.Equal(int64(len(original)), ab.Size())
				}
				if tt.override {
					algorithm := tt.algorithm
					if algorithm == "" {
						algorithm = digest.SHA256
					}
					data := original
					if tt.mutate {
						data = replacement
					}
					ab.SetPrecalculatedDigest(algorithm.FromBytes(data).String())
				}

				buffered, err := ab.Buffer()
				if tt.mutate {
					r.Error(err, "Buffer must validate the resource's expected checksum, not the replacement's actual checksum")
					r.Nil(buffered)
					return
				}
				r.NoError(err)
				r.NotNil(buffered)
				r.Same(ab.Artifact, buffered.Artifact)
				r.Equal(int64(len(original)), buffered.Size())
				checksum, known := buffered.Digest()
				r.True(known)
				r.Equal(digest.FromBytes(original).String(), checksum)
				mediaType, known := buffered.MediaType()
				r.True(known)
				r.Equal("application/octet-stream", mediaType)

				// Removing the source proves subsequent reads use the eagerly loaded cache.
				r.NoError(os.Remove(path))
				var content bytes.Buffer
				r.NoError(blob.Copy(&content, buffered))
				r.Equal(original, content.Bytes())
			})
		}
	}
}

func Test_Integration_ArtifactBlob_KnownChecksumDigestDoesNotReadFile(t *testing.T) {
	for _, normalization := range []string{internaldigest.GenericBlobDigestV1, ""} {
		t.Run(normalization, func(t *testing.T) {
			r := require.New(t)
			content := []byte("content A")
			ab, _, counted := newArtifactBlobBufferingFile(t, content, normalization)
			// Constructor validation may read the source; only subsequent lookups are measured.
			counted.opens, counted.reads = 0, 0
			r.True(ab.HasPrecalculatedDigest())
			for range 2 {
				checksum, known := ab.Digest()
				r.True(known)
				r.Equal(digest.FromBytes(content).String(), checksum)
			}
			r.Zero(counted.reads, "a known resource checksum must not reread the underlying file")
			r.Zero(counted.opens, "a known resource checksum must not reopen the underlying file")
		})
	}
}

func newArtifactBlobBufferingFile(t *testing.T, content []byte, normalization string) (*ociblob.ArtifactBlob, string, *artifactBlobCountingFS) {
	t.Helper()
	r := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "resource.bin")
	r.NoError(os.WriteFile(path, content, 0o600))
	counted := &artifactBlobCountingFS{FS: os.DirFS(dir)}
	resource := &descriptor.Resource{Digest: &descriptor.Digest{
		HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
		NormalisationAlgorithm: normalization,
		Value:                  digest.FromBytes(content).Encoded(),
	}}
	original := resource.Digest
	snapshot := *original
	t.Cleanup(func() {
		r.Same(original, resource.Digest, "the signed digest metadata must not be replaced")
		r.Equal(snapshot, *resource.Digest, "all signed digest metadata must remain unchanged")
	})
	ab, err := ociblob.NewArtifactBlobWithMediaType(resource, filesystem.NewFileBlob(counted, "resource.bin"), "application/octet-stream")
	r.NoError(err)
	return ab, path, counted
}

type artifactBlobCountingFS struct {
	fs.FS
	opens int
	reads int
}

func (f *artifactBlobCountingFS) Open(name string) (fs.File, error) {
	f.opens++
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return &artifactBlobCountingFile{File: file, owner: f}, nil
}

func (f *artifactBlobCountingFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(f.FS, name)
}

type artifactBlobCountingFile struct {
	fs.File
	owner *artifactBlobCountingFS
}

func (f *artifactBlobCountingFile) Read(p []byte) (int, error) {
	f.owner.reads++
	return f.File.Read(p)
}
