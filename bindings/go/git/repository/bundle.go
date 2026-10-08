package repository

import (
	"bufio"
	"context"
	"crypto/fips140"
	"fmt"
	"os"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const bundleSignature = "# v2 git bundle"

// DownloadGitBundle returns a complete Git object bundle for a pinned resource.
// The bundle file outlives the call and belongs to the caller.
func (r *ResourceRepository) DownloadGitBundle(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (_ blob.ReadOnlyBlob, err error) {
	spec, err := accessFrom(resource)
	if err != nil {
		return nil, err
	}
	if spec.Commit == "" {
		return nil, fmt.Errorf("git bundle requires a pinned commit")
	}
	creds, err := convertGitCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("invalid source credentials: %w", err)
	}
	options := r.downloadOptions(r.tempFolder())
	var path string
	err = download.WithRepository(ctx, spec, creds, options, func(repo *git.Repository, selected *object.Commit) error {
		pushHash := selected.Hash
		if strings.HasPrefix(spec.Ref, "refs/tags/") {
			pushHash, err = fetchPinnedTag(ctx, repo, spec.Ref, selected.Hash, spec.Repository, options, creds)
			if err != nil {
				return err
			}
		}
		_, objects, err := verifyObjectClosure(repo, pushHash)
		if err != nil {
			return err
		}
		if resource.Digest != nil {
			archiveDigest, err := download.ArchiveDigest(ctx, selected, options)
			if err != nil {
				return fmt.Errorf("cannot verify git resource digest: %w", err)
			}
			if err := verifyDigest(resource.Digest, archiveDigest); err != nil {
				return err
			}
		}
		file, err := os.CreateTemp(r.tempFolder(), "ocm-git-bundle-*")
		if err != nil {
			return fmt.Errorf("cannot create git bundle: %w", err)
		}
		path = file.Name()
		defer func() { _ = file.Close() }()
		ref := spec.Ref
		if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/tags/") {
			ref = "refs/heads/ocm-upload"
		}
		if _, err := fmt.Fprintf(file, "%s\n%s %s\n\n", bundleSignature, pushHash, ref); err != nil {
			return fmt.Errorf("cannot write git bundle header: %w", err)
		}
		hashes := make([]plumbing.Hash, 0, len(objects))
		for hash := range objects {
			hashes = append(hashes, hash)
		}
		if _, err := packfile.NewEncoder(file, repo.Storer, false).Encode(hashes, config.DefaultPackWindow); err != nil {
			return fmt.Errorf("cannot write git bundle objects: %w", err)
		}
		return file.Close()
	})
	if err != nil {
		if path != "" {
			_ = os.Remove(path)
		}
		return nil, fmt.Errorf("cannot download git bundle: %w", err)
	}
	fileBlob, err := filesystem.GetBlobFromOSPath(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return fileBlob, nil
}

// UploadResource accepts a complete Git bundle, not the snapshot tar returned
// by DownloadResource. The resource access names the target repository and ref.
func (r *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (_ *descriptor.Resource, err error) {
	spec, err := accessFrom(resource)
	if err != nil {
		return nil, err
	}
	ref := plumbing.ReferenceName(spec.Ref)
	if !strings.HasPrefix(spec.Ref, "refs/heads/") && !strings.HasPrefix(spec.Ref, "refs/tags/") {
		return nil, fmt.Errorf("target ref must be a full branch or tag ref")
	}
	if err := ref.Validate(); err != nil {
		return nil, fmt.Errorf("invalid target ref: %w", err)
	}
	if content == nil {
		return nil, fmt.Errorf("git bundle content is required")
	}
	creds, err := convertGitCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("invalid target credentials: %w", err)
	}
	stream, err := content.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("cannot read git bundle: %w", err)
	}
	defer func() { _ = stream.Close() }()
	reader := bufio.NewReaderSize(stream, 4096)
	signature, err := bundleLine(reader)
	if err != nil || signature != bundleSignature {
		return nil, fmt.Errorf("content is not a Git object bundle")
	}
	advertisement, err := bundleLine(reader)
	if err != nil {
		return nil, fmt.Errorf("invalid git bundle reference: %w", err)
	}
	fields := strings.Fields(advertisement)
	if len(fields) != 2 || len(fields[0]) != 40 || !plumbing.IsHash(fields[0]) {
		return nil, fmt.Errorf("invalid git bundle reference")
	}
	if !strings.HasPrefix(fields[1], "refs/heads/") && !strings.HasPrefix(fields[1], "refs/tags/") {
		return nil, fmt.Errorf("invalid git bundle reference")
	}
	if err := plumbing.ReferenceName(fields[1]).Validate(); err != nil {
		return nil, fmt.Errorf("invalid git bundle reference: %w", err)
	}
	if end, err := bundleLine(reader); err != nil || end != "" {
		return nil, fmt.Errorf("unsupported git bundle header")
	}
	pushHash := plumbing.NewHash(fields[0])
	dir, err := os.MkdirTemp(r.tempFolder(), "ocm-git-bundle-repository-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary git storage: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	var uploaded *descriptor.Resource
	fips140.WithoutEnforcement(func() {
		var repo *git.Repository
		repo, err = git.PlainInit(dir, true)
		if err != nil {
			return
		}
		err = packfile.UpdateObjectStorage(repo.Storer, reader)
		if err != nil {
			return
		}
		commit, peelErr := bundleCommit(repo, pushHash)
		if peelErr != nil {
			err = peelErr
			return
		}
		if spec.Commit != "" && spec.Commit != commit.String() {
			err = fmt.Errorf("git bundle commit mismatch: expected %s, got %s", spec.Commit, commit)
			return
		}
		if strings.HasPrefix(spec.Ref, "refs/heads/") {
			pushHash = commit
		}
		var commits map[plumbing.Hash]struct{}
		commits, _, err = verifyObjectClosure(repo, pushHash)
		if err != nil {
			return
		}
		if resource.Digest != nil {
			selected, readErr := object.GetCommit(repo.Storer, commit)
			if readErr != nil {
				err = readErr
				return
			}
			actualDigest, digestErr := download.ArchiveDigest(ctx, selected, r.downloadOptions(r.tempFolder()))
			err = digestErr
			if err != nil {
				return
			}
			err = verifyDigest(resource.Digest, actualDigest)
			if err != nil {
				return
			}
		}
		uploaded, err = r.pushGitRepository(ctx, repo, resource, spec.Repository, ref, commit, pushHash, commits, creds, r.downloadOptions(r.tempFolder()))
	})
	if err != nil {
		return nil, fmt.Errorf("cannot upload git bundle: %w", err)
	}
	return uploaded, nil
}

func bundleLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(line), "\n"), nil
}

func bundleCommit(repo *git.Repository, hash plumbing.Hash) (plumbing.Hash, error) {
	for range 32 {
		encoded, err := repo.Storer.EncodedObject(plumbing.AnyObject, hash)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("cannot read git bundle tip: %w", err)
		}
		switch encoded.Type() {
		case plumbing.CommitObject:
			return hash, nil
		case plumbing.TagObject:
			tag, err := object.GetTag(repo.Storer, hash)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			hash = tag.Target
		default:
			return plumbing.ZeroHash, fmt.Errorf("git bundle tip is not a commit or tag")
		}
	}
	return plumbing.ZeroHash, fmt.Errorf("git bundle tag nesting exceeds the limit")
}
