package download

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// treeFS serves a Git tree as a read-only file system without a host checkout.
// Directory listings are sorted by name; gitlinks are empty directories, as their
// target objects need not be present.
type treeFS struct{ tree *object.Tree }

var (
	_ fs.ReadDirFS  = treeFS{}
	_ fs.ReadLinkFS = treeFS{}
)

func (f treeFS) Open(name string) (fs.File, error) {
	info, err := f.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return &treeFile{info: info}, nil
	}
	file, err := f.tree.File(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, err
	}
	return &treeFile{ReadCloser: reader, info: info}, nil
}

func (f treeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	tree := f.tree
	if name != "." {
		info, err := f.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
		}
		if tree, err = f.tree.Tree(name); errors.Is(err, object.ErrDirectoryNotFound) {
			return nil, nil // gitlink
		} else if err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
		}
	}

	entries := make([]fs.DirEntry, 0, len(tree.Entries))
	for i := range tree.Entries {
		info, err := entryInfo(tree, path.Join(name, tree.Entries[i].Name), &tree.Entries[i])
		if err != nil {
			return nil, err
		}
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func (f treeFS) ReadLink(name string) (string, error) {
	file, err := f.tree.File(name)
	if err != nil {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: err}
	}
	return file.Contents()
}

func (f treeFS) Lstat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return treeInfo(name, filemode.Dir, 0), nil
	}
	entry, err := f.tree.FindEntry(name)
	if err != nil {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: err}
	}
	return entryInfo(f.tree, name, entry)
}

// entryInfo describes a tree entry; gitlinks and directories have no size.
func entryInfo(tree *object.Tree, name string, entry *object.TreeEntry) (fs.FileInfo, error) {
	var size int64
	if entry.Mode != filemode.Dir && entry.Mode != filemode.Submodule {
		file, err := tree.TreeEntryFile(entry)
		if err != nil {
			return nil, err
		}
		size = file.Size
	}
	return treeInfo(name, entry.Mode, size), nil
}

type treeFile struct {
	io.ReadCloser // nil for directories
	info          fs.FileInfo
}

func (f *treeFile) Read(p []byte) (int, error) {
	if f.ReadCloser == nil {
		return 0, &fs.PathError{Op: "read", Path: f.info.Name(), Err: fs.ErrInvalid}
	}
	return f.ReadCloser.Read(p)
}

func (f *treeFile) Close() error {
	if f.ReadCloser == nil {
		return nil
	}
	return f.ReadCloser.Close()
}

func (f *treeFile) Stat() (fs.FileInfo, error) { return f.info, nil }

type treeFileInfo struct {
	name string
	mode fs.FileMode
	size int64
}

func treeInfo(name string, mode filemode.FileMode, size int64) fs.FileInfo {
	info := treeFileInfo{name: path.Base(name), mode: 0o644, size: size}
	// Git's deprecated regular-file mode must not leak group-write permission.
	switch mode {
	case filemode.Dir, filemode.Submodule:
		info.mode = fs.ModeDir | 0o755
	case filemode.Symlink:
		info.mode = fs.ModeSymlink | 0o777
	case filemode.Executable:
		info.mode = 0o755
	}
	return info
}

func (i treeFileInfo) Name() string       { return i.name }
func (i treeFileInfo) Size() int64        { return i.size }
func (i treeFileInfo) Mode() fs.FileMode  { return i.mode }
func (i treeFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (i treeFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i treeFileInfo) Sys() any           { return nil }
