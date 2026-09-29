package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/ctf"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitaccess "ocm.software/open-component-model/bindings/go/git/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
)

// A local repository exercises real Git objects without Docker, network access,
// or host Git configuration deciding how fixture commits are signed.
func newCLIGitFixture(t *testing.T) (string, string) {
	t.Helper()
	r := require.New(t)
	path := t.TempDir()
	repo, err := git.PlainInit(path, false)
	r.NoError(err)
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
	tree, err := repo.Worktree()
	r.NoError(err)
	var first string
	for _, content := range []string{"first\n", "second\n"} {
		r.NoError(os.WriteFile(filepath.Join(path, "README.md"), []byte(content), 0o600))
		_, err := tree.Add("README.md")
		r.NoError(err)
		hash, err := tree.Commit(content, &git.CommitOptions{Author: &object.Signature{Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC()}})
		r.NoError(err)
		if first == "" {
			first = hash.String()
		}
	}
	return path, first
}

func Test_Integration_GitInputDownloadAndTransfer(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	gitPath, commit := newCLIGitFixture(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600))
	run := func(t *testing.T, args ...string) {
		t.Helper()
		r := require.New(t)
		command := cmd.New()
		command.SetArgs(append(args, "--config", cfgPath))
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		r.NoError(command.ExecuteContext(ctx))
	}
	const name, version = "ocm.software/git-cli", "1.0.0"
	constructorPath := filepath.Join(dir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(fmt.Sprintf(`components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: input
    version: 1.0.0
    type: directoryTree
    relation: local
    input:
      type: Git/v1
      repository: %q
      ref: refs/heads/main
      commit: %s
  - name: access
    version: 1.0.0
    type: directoryTree
    relation: external
    access:
      type: Git/v1
      repository: %q
      ref: refs/heads/main
      commit: %s
`, name, version, gitPath, commit, gitPath, commit)), 0o600))
	sourcePath := filepath.Join(dir, "source")
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourcePath, name, version)
	run(t, "add", "component-version", "--repository", "ctf::"+sourcePath, "--constructor", constructorPath)
	openCTF := func(t *testing.T, path string) *oci.Repository {
		t.Helper()
		r := require.New(t)
		fs, err := filesystem.NewFS(path, os.O_RDONLY)
		r.NoError(err)
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
		r.NoError(err)
		return repo
	}
	source := openCTF(t, sourcePath)
	desc, err := source.GetComponentVersion(ctx, name, version)
	r.NoError(err)
	r.Len(desc.Component.Resources, 2)
	input, access := desc.Component.Resources[0], desc.Component.Resources[1]
	r.Equal("input", input.Name)
	r.Equal(v2.LocalBlobAccessType, input.Access.GetType().Name)
	r.Equal("access", access.Name)
	var pinned accessv1.Git
	r.NoError(gitaccess.Scheme.Convert(access.Access, &pinned))
	r.Equal(commit, pinned.Commit)
	r.NotNil(input.Digest)
	r.NotNil(access.Digest)
	r.Equal("SHA-256", access.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", access.Digest.NormalisationAlgorithm)
	inputBytes := readLocalResource(t, ctx, source, name, version, input.ToIdentity())
	r.Equal(godigest.FromBytes(inputBytes).Encoded(), input.Digest.Value)
	assertCLIGitArchive(t, inputBytes)
	output := filepath.Join(dir, "download.tar.gz")
	run(t, "download", "resource", sourceRef, "--identity", "name=access", "--output", output)
	downloaded, err := os.ReadFile(output)
	r.NoError(err)
	assertCLIGitArchive(t, downloaded)
	r.Equal(inputBytes, downloaded)
	r.Equal(godigest.FromBytes(downloaded).Encoded(), access.Digest.Value)

	t.Run("by value", func(t *testing.T) {
		r := require.New(t)
		targetPath := filepath.Join(dir, "target")
		run(t, "transfer", "component-version", sourceRef, "ctf::"+targetPath, "--copy-resources")
		target := openCTF(t, targetPath)
		transferred, err := target.GetComponentVersion(ctx, name, version)
		r.NoError(err)
		r.Len(transferred.Component.Resources, 2)
		for _, res := range transferred.Component.Resources {
			var local v2.LocalBlob
			r.NoError(v2.Scheme.Convert(res.Access, &local))
			r.Equal("application/x-tgz", local.MediaType)
			r.Equal(access.Digest, res.Digest)
			stored := readLocalResource(t, ctx, target, name, version, res.ToIdentity())
			r.Equal(downloaded, stored)
		}
	})
	t.Run("default preserves external access", func(t *testing.T) {
		r := require.New(t)
		targetPath := filepath.Join(dir, "reference-target")
		run(t, "transfer", "component-version", sourceRef, "ctf::"+targetPath)
		transferred, err := openCTF(t, targetPath).GetComponentVersion(ctx, name, version)
		r.NoError(err)
		r.Len(transferred.Component.Resources, 2)
		r.Equal("Git/v1", transferred.Component.Resources[1].Access.GetType().String())
		r.Equal(access.Digest, transferred.Component.Resources[1].Digest)
	})
}

func assertCLIGitArchive(t *testing.T, data []byte) {
	t.Helper()
	r := require.New(t)
	gz, err := gzip.NewReader(bytes.NewReader(data))
	r.NoError(err)
	defer func() { r.NoError(gz.Close()) }()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	r.NoError(err)
	r.Equal("README.md", header.Name)
	content, err := io.ReadAll(tr)
	r.NoError(err)
	r.Equal("first\n", string(content), "commit must take precedence over the newer main branch")
	_, err = tr.Next()
	r.ErrorIs(err, io.EOF)
}
