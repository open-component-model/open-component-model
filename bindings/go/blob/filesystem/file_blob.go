package filesystem

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Blob is a blob.Blob that is stored in a fs.FS.
// It delegates all meta-operations to the underlying filesystem.
type Blob struct {
	// fileSystem is the underlying filesystem.
	fileSystem fs.FS
	// path is the original path to the blob.
	path string
	// mediaType is the media type of the blob.
	mediaType atomic.Pointer[string]
}

var (
	_ blob.Blob                  = (*Blob)(nil)
	_ blob.SizeAware             = (*Blob)(nil)
	_ blob.DigestAware           = (*Blob)(nil)
	_ blob.MediaTypeAware        = (*Blob)(nil)
	_ blob.MediaTypeOverrideable = (*Blob)(nil)
)

func NewFileBlobFromPathWithFlag(path string, flag int) (*Blob, error) {
	path = filepath.Clean(path)
	dir, base := filepath.Dir(path), filepath.Base(path)
	rofs, err := NewFS(dir, flag)
	if err != nil {
		return nil, fmt.Errorf("failed to setup filesystem in %q while trying to create file blob %q: %w", dir, base, err)
	}
	return NewFileBlob(rofs, base), nil
}

// NewFileBlob creates a new Blob from an underlying fs.FS.
func NewFileBlob(fs fs.FS, path string) *Blob {
	return &Blob{
		path:       path,
		fileSystem: fs,
	}
}

func (f *Blob) ReadCloser() (io.ReadCloser, error) {
	file, err := f.fileSystem.Open(f.path)
	if err != nil {
		return nil, fmt.Errorf("unable to open file %q: %w", f.path, err)
	}
	return file, nil
}

func (f *Blob) WriteCloser() (io.WriteCloser, error) {
	statFS, ok := f.fileSystem.(fs.StatFS)
	if !ok {
		return nil, fmt.Errorf("filesystem %T does not support stat", f.fileSystem)
	}
	fi, err := statFS.Stat(f.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("unable to stat file %q: %w", f.path, err)
	}
	var mode fs.FileMode = 0o600
	if err == nil && fi.Mode()&fs.ModeNamedPipe != 0 {
		mode = fs.ModeNamedPipe
	}
	ofFS, ok := f.fileSystem.(OpenFileFS)
	if !ok {
		return nil, fmt.Errorf("filesystem %T does not support open file", f.fileSystem)
	}

	file, err := ofFS.OpenFile(f.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
	if err != nil {
		return nil, err
	}
	writeable, ok := file.(io.WriteCloser)
	if !ok {
		return nil, errors.New("file is read only")
	}
	return writeable, nil
}

func (f *Blob) Size() int64 {
	statFS, ok := f.fileSystem.(fs.StatFS)
	if !ok {
		return blob.SizeUnknown
	}
	fi, err := statFS.Stat(f.path)
	if err != nil {
		return blob.SizeUnknown
	}
	return fi.Size()
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

// MediaType returns the media type of the blob if known.
func (f *Blob) MediaType() (string, bool) {
	mt := f.mediaType.Load()
	if mt == nil {
		return "", false
	}
	return *mt, true
}

// SetMediaType overrides the media type of the blob.
func (f *Blob) SetMediaType(mediaType string) {
	f.mediaType.Store(&mediaType)
}
