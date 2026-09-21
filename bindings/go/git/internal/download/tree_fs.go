package download

import (
	"io"
	"io/fs"
	"path"
	"time"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// treeFS provides object contents to the shared archiver without a host checkout.
// Traversal and directory metadata come from the Git tree walker, not Open.
type treeFS struct{ tree *object.Tree }

func (f treeFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	file, err := f.tree.File(name)
	if err != nil {
		return nil, err
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, err
	}
	return &treeFile{ReadCloser: reader, info: treeInfo(name, file.Mode, file.Size)}, nil
}

func (f treeFS) Readlink(name string) (string, error) {
	file, err := f.tree.File(name)
	if err != nil {
		return "", err
	}
	return file.Contents()
}

type treeFile struct {
	io.ReadCloser
	info fs.FileInfo
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
	case filemode.Dir:
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
