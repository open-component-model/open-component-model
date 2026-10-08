package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
)

var (
	// errIncompleteObjects reports an object missing from the history of a tip.
	errIncompleteObjects = errors.New("incomplete git object history")
	// ErrNoHistory reports an archive without Git storage, such as an OCM v1 snapshot.
	ErrNoHistory = errors.New("the resource is a files-only snapshot (e.g. created by OCM v1); re-construct it with OCM v2")
)

// maxHeadSize bounds the HEAD file read from an archive.
const maxHeadSize = 4096

// VerifyObjectClosure walks the history of tip and fails on the first missing
// object. It returns the commits and all objects of that history; submodule
// commits are not part of it.
func VerifyObjectClosure(repo *git.Repository, tip plumbing.Hash) (map[plumbing.Hash]struct{}, map[plumbing.Hash]struct{}, error) {
	seen := make(map[plumbing.Hash]struct{})
	commits := make(map[plumbing.Hash]struct{})
	pending := []plumbing.Hash{tip}
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		encoded, err := repo.Storer.EncodedObject(plumbing.AnyObject, hash)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: object %s: %w", errIncompleteObjects, hash, err)
		}
		switch encoded.Type() {
		case plumbing.CommitObject:
			commit, err := object.GetCommit(repo.Storer, hash)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot read commit %s: %w", hash, err)
			}
			commits[hash] = struct{}{}
			pending = append(pending, commit.TreeHash)
			pending = append(pending, commit.ParentHashes...)
		case plumbing.TreeObject:
			tree, err := object.GetTree(repo.Storer, hash)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot read tree %s: %w", hash, err)
			}
			for _, entry := range tree.Entries {
				if entry.Mode != filemode.Submodule {
					pending = append(pending, entry.Hash)
				}
			}
		case plumbing.TagObject:
			tag, err := object.GetTag(repo.Storer, hash)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot read tag %s: %w", hash, err)
			}
			pending = append(pending, tag.Target)
		case plumbing.BlobObject:
		default:
			return nil, nil, fmt.Errorf("unsupported git object type %s", encoded.Type())
		}
	}
	return commits, seen, nil
}

// Import stores the Git objects of an archive written by Download in a new bare
// repository in dir and returns the commit its HEAD names. Packfiles are parsed
// again, so every object is stored under the hash of its content. Other entries
// are skipped, and reading stops at the end of the tar archive.
func Import(ctx context.Context, r io.Reader, dir string) (*git.Repository, plumbing.Hash, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("git archive is not gzip-compressed: %w", err)
	}
	defer func() {
		if err := gz.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close git archive reader", "err", err)
		}
	}()

	repo, err := git.PlainInit(dir, true)
	if err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("cannot create git repository: %w", err)
	}

	history := false
	head := plumbing.ZeroHash
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, plumbing.ZeroHash, fmt.Errorf("cannot read git archive: %w", err)
		}
		rel, ok := gitPath(header.Name)
		if !ok {
			continue
		}
		history = true
		if header.Typeflag != tar.TypeReg {
			continue
		}
		switch {
		case strings.HasPrefix(rel, "objects/pack/") && strings.HasSuffix(rel, ".pack"):
			err = packfile.UpdateObjectStorage(repo.Storer, tr)
		case rel == "HEAD":
			head, err = readHead(tr)
		}
		if err != nil {
			return nil, plumbing.ZeroHash, fmt.Errorf("invalid git archive entry %q: %w", header.Name, err)
		}
	}
	if !history {
		return nil, plumbing.ZeroHash, ErrNoHistory
	}
	if head.IsZero() {
		return nil, plumbing.ZeroHash, fmt.Errorf("git archive HEAD does not name a commit")
	}
	return repo, head, nil
}

func readHead(r io.Reader) (plumbing.Hash, error) {
	content, err := io.ReadAll(io.LimitReader(r, maxHeadSize))
	if err != nil {
		return plumbing.ZeroHash, err
	}
	hash, ok := plumbing.FromHex(strings.TrimSpace(string(content)))
	if !ok {
		return plumbing.ZeroHash, fmt.Errorf("git archive HEAD is not detached at a commit")
	}
	return hash, nil
}
