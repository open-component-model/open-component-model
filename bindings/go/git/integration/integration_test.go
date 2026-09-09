package integration_test

import (
	"archive/tar"
	"bytes"
	"encoding/pem"
	"io"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_Git downloads real Git objects over HTTPS through git http-backend.
func Test_Integration_Git(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	executable, err := exec.LookPath("git")
	r.NoError(err)

	server := httptest.NewTLSServer(&cgi.Handler{
		Path: executable,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(fixture.Path), "GIT_HTTP_EXPORT_ALL=1"},
	})
	t.Cleanup(server.Close)

	url := server.URL + "/" + filepath.Base(fixture.Path)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	tempDir := t.TempDir()
	opts := []repository.Option{repository.WithCABundle(ca), repository.WithTempDir(tempDir)}
	repo := repository.NewResourceRepository(opts...)
	resourceFor := func(t *testing.T, ref, commit string) *descriptor.Resource {
		t.Helper()

		raw := &runtime.Raw{}
		require.NoError(t, access.Scheme.Convert(&accessv1.Git{
			Type:       runtime.NewVersionedType("git", "v1alpha1"),
			Repository: url,
			Ref:        ref,
			Commit:     commit,
		}, raw))
		return &descriptor.Resource{Access: raw}
	}

	t.Run("branches tags and commits", func(t *testing.T) {
		for _, tc := range []struct {
			name, ref, commit, content string
		}{
			{"HEAD", "HEAD", "", "second\n"},
			{"branch", "main", "", "second\n"},
			{"full branch", "refs/heads/main", "", "second\n"},
			{"lightweight tag", "v1", "", "first\n"},
			{"annotated tag", "refs/tags/annotated", "", "first\n"},
			{"pinned commit", "", fixture.First.String(), "first\n"},
			{"informational ref", "refs/heads/deleted", fixture.First.String(), "first\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := require.New(t)

				b, err := repo.DownloadResource(t.Context(), resourceFor(t, tc.ref, tc.commit), nil)
				r.NoError(err)
				assertArchive(t, b, tc.content)
			})
		}
	})

	t.Run("pin and verify after branch movement", func(t *testing.T) {
		r := require.New(t)

		r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.First)))
		t.Cleanup(func() {
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.Second)))
		})

		original := resourceFor(t, "refs/heads/main", "")
		before := original.DeepCopy()
		pinned, err := repo.ProcessResourceDigest(t.Context(), original, nil)
		r.NoError(err)
		r.Equal(before, original)

		var spec accessv1.Git
		r.NoError(access.Scheme.Convert(pinned.Access, &spec))
		r.Equal(fixture.First.String(), spec.Commit)
		r.Equal("refs/heads/main", spec.Ref)
		r.Equal("SHA-256", pinned.Digest.HashAlgorithm)
		r.Equal("genericBlobDigest/v1", pinned.Digest.NormalisationAlgorithm)
		r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.Second)))

		first, err := repo.DownloadResource(t.Context(), pinned, nil)
		r.NoError(err)

		firstBytes := assertArchive(t, first, "first\n")
		r.Equal(digest.FromBytes(firstBytes).Encoded(), pinned.Digest.Value)

		again, err := repo.DownloadResource(t.Context(), pinned, nil)
		r.NoError(err)
		r.Equal(firstBytes, assertArchive(t, again, "first\n"))

		verified, err := repo.ProcessResourceDigest(t.Context(), pinned, nil)
		r.NoError(err)
		r.Equal(pinned, verified)

		mismatched := pinned.DeepCopy()
		mismatched.Digest.Value = strings.Repeat("0", 64)
		_, err = repo.DownloadResource(t.Context(), mismatched, nil)
		r.ErrorContains(err, "digest mismatch")

		_, err = repo.ProcessResourceDigest(t.Context(), mismatched, nil)
		r.ErrorContains(err, "digest mismatch")
	})

	t.Run("missing revision and output limit", func(t *testing.T) {
		r := require.New(t)

		_, err := repo.DownloadResource(t.Context(), resourceFor(t, "missing-ref", ""), nil)
		r.Error(err)

		limited := repository.NewResourceRepository(append(opts, repository.WithMaxDownloadSize(1))...)
		_, err = limited.DownloadResource(t.Context(), resourceFor(t, "main", ""), nil)
		r.ErrorContains(err, "maximum download size")
	})

	entries, err := os.ReadDir(tempDir)
	r.NoError(err)
	r.Empty(entries, "downloads and digest processing must remove temporary files")
}

func assertArchive(t *testing.T, content blob.ReadOnlyBlob, expectedReadme string) []byte {
	t.Helper()

	r := require.New(t)

	defer func() { r.NoError(content.(io.Closer).Close()) }()
	reader, err := content.ReadCloser()
	r.NoError(err)

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.NoError(reader.Close())

	mediaType, ok := content.(blob.MediaTypeAware).MediaType()
	r.True(ok)
	r.Equal("application/x-tar", mediaType)
	r.Equal(int64(len(data)), content.(blob.SizeAware).Size())

	checksum, ok := content.(blob.DigestAware).Digest()
	r.True(ok)
	r.Equal(digest.FromBytes(data).String(), checksum)

	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		names = append(names, header.Name)
		payload, err := io.ReadAll(tr)
		r.NoError(err)

		switch header.Name {
		case "README.md":
			r.Equal(expectedReadme, string(payload))
		case "docs/guide.txt":
			r.Equal("guide\n", string(payload))
		case "run.sh":
			r.Equal("#!/bin/sh\necho fixture\n", string(payload))
			r.NotZero(header.Mode & 0o111)
		case "link":
			r.Equal(byte(tar.TypeSymlink), header.Typeflag)
			r.Equal("docs/guide.txt", header.Linkname)
			r.Empty(payload)
		}
	}
	r.Equal([]string{"README.md", "docs", "docs/guide.txt", "link", "run.sh"}, names)

	return data
}
