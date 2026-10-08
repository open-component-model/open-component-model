package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/revlist"
)

var (
	// errIncompleteObjects reports an object missing from the history of a tip.
	errIncompleteObjects = errors.New("incomplete git object history")
	// ErrNoHistory reports an archive without Git storage, such as an OCM v1 snapshot.
	ErrNoHistory = errors.New("the resource is a files-only snapshot (e.g. created by OCM v1); re-construct it with OCM v2")
)

// maxHeadSize bounds the HEAD file read from an archive.
const maxHeadSize = 4096

// HistoryObjects returns the objects reachable from tip in hash order and
// fails if one of them is missing. revlist only lists blobs from their trees,
// so each object is also looked up. Submodule commits are not part of it.
func HistoryObjects(repo *git.Repository, tip plumbing.Hash) ([]plumbing.Hash, error) {
	hashes, err := revlist.Objects(repo.Storer, []plumbing.Hash{tip}, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errIncompleteObjects, err)
	}
	for _, hash := range hashes {
		if err := repo.Storer.HasEncodedObject(hash); err != nil {
			return nil, fmt.Errorf("%w: object %s: %w", errIncompleteObjects, hash, err)
		}
	}
	sort.Sort(plumbing.HashSlice(hashes))
	return hashes, nil
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

	history, packed := false, false
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
			if packed {
				return nil, plumbing.ZeroHash, fmt.Errorf("git archive holds more than one packfile")
			}
			packed = true
			err = packfile.UpdateObjectStorage(repo.Storer, tr)
		case rel == "HEAD":
			if !head.IsZero() {
				return nil, plumbing.ZeroHash, fmt.Errorf("git archive holds more than one HEAD")
			}
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
