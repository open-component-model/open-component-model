package filesystem

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Blob is a blob.Blob that is stored in a fs.FS.
// It delegates all meta operations to the underlying filesystem.
type Blob struct {
	// fileSystem is the underlying filesystem.
	fileSystem FileSystem
	// path is the original path to the blob.
	path string
}

var (
	_ blob.Blob        = (*Blob)(nil)
	_ blob.SizeAware   = (*Blob)(nil)
	_ blob.DigestAware = (*Blob)(nil)
)

func NewFileBlob(fs FileSystem, path string) *Blob {
	return &Blob{
		path:       path,
		fileSystem: fs,
	}
}

func (f *Blob) ReadCloser() (io.ReadCloser, error) {
	file, err := f.fileSystem.OpenFile(f.path, os.O_RDONLY, 0o400)
	if err != nil {
		return nil, fmt.Errorf("unable to open file %q: %w", f.path, err)
	}
	return file, nil
}

func (f *Blob) WriteCloser() (io.WriteCloser, error) {
	fi, err := f.fileSystem.Stat(f.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("unable to stat file %q: %w", f.path, err)
	}
	var mode fs.FileMode = 0o600
	if err == nil && fi.Mode()&fs.ModeNamedPipe != 0 {
		mode = fs.ModeNamedPipe
	}
	file, err := f.fileSystem.OpenFile(f.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
	if err != nil {
		return nil, err
	}
	writeable, ok := file.(io.WriteCloser)
	if !ok {
		return nil, errors.New("file is read only")
	}
	return writeable, nil
}

func (f *Blob) Size() (uint64, bool) {
	fi, err := f.fileSystem.Stat(f.path)
	if err != nil {
		return 0, false
	}
	return uint64(fi.Size()), true //nolint:gosec // G115 can never be negative
}

func (f *Blob) Digest() (string, bool) {
	data, err := f.ReadCloser()
	if err != nil {
		return "", false
	}
	defer func() {
		_ = data.Close()
	}()
	var buf bytes.Buffer
	d, err := digest.FromReader(io.TeeReader(data, &buf))
	if err != nil {
		return "", false
	}
	return d.String(), true
}
