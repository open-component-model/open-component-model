package repository_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUploadResourceRoundTrip(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	targetPath := filepath.Join(t.TempDir(), "target.git")
	_, err := git.PlainInit(targetPath, true)
	r.NoError(err)
	uploader := newUploader(t)

	for _, testCase := range []struct {
		name      string
		sourceRef string
		commit    plumbing.Hash
		targetRef string
		wantHash  plumbing.Hash
		wantErr   string
	}{
		{name: "initial upload", sourceRef: "refs/heads/main", commit: fixture.First, targetRef: "refs/heads/main", wantHash: fixture.First},
		{name: "idempotent upload", sourceRef: "refs/heads/main", commit: fixture.First, targetRef: "refs/heads/main", wantHash: fixture.First},
		{name: "fast-forward update", sourceRef: "refs/heads/main", commit: fixture.Second, targetRef: "refs/heads/main", wantHash: fixture.Second},
		{name: "non-fast-forward update", sourceRef: "refs/heads/main", commit: fixture.First, targetRef: "refs/heads/main", wantHash: fixture.Second, wantErr: "does not fast-forward"},
		{name: "branch to another branch", sourceRef: "refs/heads/main", commit: fixture.Second, targetRef: "refs/heads/release", wantHash: fixture.Second},
		{name: "annotated tag to lightweight tag", sourceRef: "refs/tags/annotated", commit: fixture.First, targetRef: "refs/tags/copied", wantHash: fixture.First},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			source, content := downloadResource(t, uploader, fixture.Path, testCase.sourceRef, testCase.commit)
			uploaded, err := uploader.UploadResource(t.Context(), targetResource(source, targetPath, testCase.targetRef), content, nil)
			if testCase.wantErr != "" {
				r.ErrorContains(err, testCase.wantErr)
			} else {
				r.NoError(err)
				r.Equal(&accessv1.Git{
					Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
					Repository: targetPath,
					Ref:        testCase.targetRef,
					Commit:     testCase.commit.String(),
				}, uploaded.Access)
				verified, err := uploader.ProcessResourceDigest(t.Context(), uploaded, nil)
				r.NoError(err, "the uploaded resource verifies when downloaded from the target")
				r.Equal(source.Digest, verified.Digest)
			}

			target, err := git.PlainOpen(targetPath)
			r.NoError(err)
			ref, err := target.Reference(plumbing.ReferenceName(testCase.targetRef), false)
			r.NoError(err)
			r.Equal(testCase.wantHash, ref.Hash())
			commit, err := target.CommitObject(testCase.commit)
			r.NoError(err)
			original, err := fixture.Git.CommitObject(testCase.commit)
			r.NoError(err)
			r.Equal(original.Author, commit.Author)
			r.Equal(original.ParentHashes, commit.ParentHashes)
		})
	}
}

func TestUploadResourceToExistingAnnotatedTag(t *testing.T) {
	fixture := newRepository(t)
	uploader := newUploader(t)

	for _, testCase := range []struct {
		name    string
		commit  plumbing.Hash
		wantErr string
	}{
		{name: "tag at the uploaded commit", commit: fixture.First},
		{name: "tag at another commit", commit: fixture.Second, wantErr: "already exists at another commit"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			targetPath := filepath.Join(t.TempDir(), "target.git")
			_, err := git.PlainClone(targetPath, &git.CloneOptions{URL: fixture.Path, Bare: true, Tags: git.AllTags})
			r.NoError(err)
			target, err := git.PlainOpen(targetPath)
			r.NoError(err)
			// fixture.Path tags fixture.First as an annotated tag, as if it had been pushed by hand.
			before, err := target.Reference("refs/tags/annotated", false)
			r.NoError(err)

			source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", testCase.commit)
			_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/tags/annotated"), content, nil)
			if testCase.wantErr != "" {
				r.ErrorContains(err, testCase.wantErr)
			} else {
				r.NoError(err)
			}
			after, err := target.Reference("refs/tags/annotated", false)
			r.NoError(err)
			r.Equal(before.Hash(), after.Hash(), "the existing tag is left as it is")
		})
	}
}

func TestDownloadResourceDependsOnCommitOnly(t *testing.T) {
	fixture := newRepository(t)
	want, _ := downloadResource(t, newUploader(t), fixture.Path, "refs/heads/main", fixture.First)

	for _, testCase := range []struct {
		name string
		ref  string
	}{
		{name: "repeated download", ref: "refs/heads/main"},
		{name: "annotated tag ref", ref: "refs/tags/annotated"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			got, _ := downloadResource(t, newUploader(t), fixture.Path, testCase.ref, fixture.First)
			r.Equal(want.Digest, got.Digest)
		})
	}
}

func TestUploadResourceRejectsArchive(t *testing.T) {
	fixture := newRepository(t)
	uploader := newUploader(t)
	source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", fixture.Second)

	for _, testCase := range []struct {
		name    string
		digest  string
		content blob.ReadOnlyBlob
		wantErr string
	}{
		{
			name:    "digest mismatch",
			digest:  strings.Repeat("0", 64),
			content: content,
			wantErr: "digest mismatch",
		},
		{
			name: "files-only snapshot",
			content: rewriteArchive(t, content, func(name string, data []byte) []archiveEntry {
				if name == ".git" || strings.HasPrefix(name, ".git/") {
					return nil
				}
				return []archiveEntry{{name: name, data: data}}
			}),
			wantErr: "git upload needs history: the resource is a files-only snapshot (e.g. created by OCM v1); re-construct it with OCM v2",
		},
		{
			name: "missing tree",
			content: rewriteArchive(t, content, func(name string, data []byte) []archiveEntry {
				if strings.HasSuffix(name, ".pack") {
					data = packWithout(t, fixture.Git, fixture.Second, func(typ plumbing.ObjectType) bool { return typ == plumbing.TreeObject })
				}
				return []archiveEntry{{name: name, data: data}}
			}),
			wantErr: "incomplete git object history",
		},
		{
			name: "missing blob",
			content: rewriteArchive(t, content, func(name string, data []byte) []archiveEntry {
				if strings.HasSuffix(name, ".pack") {
					data = packWithout(t, fixture.Git, fixture.Second, func(typ plumbing.ObjectType) bool { return typ == plumbing.BlobObject })
				}
				return []archiveEntry{{name: name, data: data}}
			}),
			wantErr: "incomplete git object history",
		},
		{
			name: "second packfile",
			content: rewriteArchive(t, content, func(name string, data []byte) []archiveEntry {
				if strings.HasSuffix(name, ".pack") {
					return []archiveEntry{{name: name, data: data}, {name: strings.TrimSuffix(name, ".pack") + "-copy.pack", data: data}}
				}
				return []archiveEntry{{name: name, data: data}}
			}),
			wantErr: "git archive holds more than one packfile",
		},
		{
			name: "second HEAD",
			content: rewriteArchive(t, content, func(name string, data []byte) []archiveEntry {
				if name == ".git/HEAD" {
					return []archiveEntry{{name: name, data: data}, {name: name, data: data}}
				}
				return []archiveEntry{{name: name, data: data}}
			}),
			wantErr: "git archive holds more than one HEAD",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			targetPath := filepath.Join(t.TempDir(), "target.git")
			target, err := git.PlainInit(targetPath, true)
			r.NoError(err)
			resource := targetResource(source, targetPath, "refs/heads/main")
			resource.Digest = nil
			if testCase.digest != "" {
				resource.Digest = source.Digest.DeepCopy()
				resource.Digest.Value = testCase.digest
			}

			_, err = uploader.UploadResource(t.Context(), resource, testCase.content, nil)
			r.ErrorContains(err, testCase.wantErr)
			_, err = target.Reference("refs/heads/main", false)
			r.ErrorIs(err, plumbing.ErrReferenceNotFound)
		})
	}
}

func newUploader(t *testing.T) *repository.ResourceRepository {
	t.Helper()
	tempDir := t.TempDir()
	return repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})
}

// downloadResource pins and digests a source resource and returns its archive.
func downloadResource(t *testing.T, repo *repository.ResourceRepository, path, ref string, commit plumbing.Hash) (*descriptor.Resource, blob.ReadOnlyBlob) {
	t.Helper()
	r := require.New(t)
	source, err := repo.ProcessResourceDigest(t.Context(), &descriptor.Resource{Access: &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: path,
		Ref:        ref,
		Commit:     commit.String(),
	}}, nil)
	r.NoError(err)
	content, err := repo.DownloadResource(t.Context(), source, nil)
	r.NoError(err)
	return source, content
}

func targetResource(source *descriptor.Resource, path, ref string) *descriptor.Resource {
	target := source.DeepCopy()
	target.Access = &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: path,
		Ref:        ref,
	}
	return target
}

type archiveEntry struct {
	name string
	data []byte
}

// rewriteArchive copies a tar.gz, replacing each entry with what edit returns.
func rewriteArchive(t *testing.T, content blob.ReadOnlyBlob, edit func(name string, data []byte) []archiveEntry) blob.ReadOnlyBlob {
	t.Helper()
	r := require.New(t)
	reader, err := content.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()
	gz, err := gzip.NewReader(reader)
	r.NoError(err)

	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	tw := tar.NewWriter(gw)
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		data, err := io.ReadAll(tr)
		r.NoError(err)
		for _, entry := range edit(header.Name, data) {
			header.Name, header.Size = entry.name, int64(len(entry.data))
			r.NoError(tw.WriteHeader(header))
			_, err = tw.Write(entry.data)
			r.NoError(err)
		}
	}
	r.NoError(tw.Close())
	r.NoError(gw.Close())
	return inmemory.New(bytes.NewReader(out.Bytes()))
}

// packWithout encodes the history of commit without the objects drop rejects.
func packWithout(t *testing.T, repo *git.Repository, commit plumbing.Hash, drop func(plumbing.ObjectType) bool) []byte {
	t.Helper()
	r := require.New(t)
	hashes, err := revlist.Objects(repo.Storer, []plumbing.Hash{commit}, nil)
	r.NoError(err)
	kept := slices.DeleteFunc(hashes, func(hash plumbing.Hash) bool {
		o, err := repo.Storer.EncodedObject(plumbing.AnyObject, hash)
		r.NoError(err)
		return drop(o.Type())
	})
	var pack bytes.Buffer
	_, err = packfile.NewEncoder(&pack, repo.Storer, false).Encode(kept, config.DefaultPackWindow)
	r.NoError(err)
	return pack.Bytes()
}
