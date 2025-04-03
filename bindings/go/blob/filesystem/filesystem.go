package filesystem

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

var ErrReadOnly = errors.New("read only file system")

// FileSystem is an interface that needs to be fulfilled by any filesystem implementation
// to be usable within the OCM Bindings.
// The ComponentVersionReference Implementation is the OSFileSystem which is backed by the os package.
type FileSystem interface {
	Base() string
	Open(name string) (fs.File, error)
	OpenFile(name string, flag int, perm os.FileMode) (fs.File, error)
	MkdirAll(name string, perm os.FileMode) error
	Remove(name string) error
	ReadDir(name string) ([]fs.DirEntry, error)
	RemoveAll(path string) error
	Stat(name string) (fs.FileInfo, error)
	ReadOnly() bool
	ForceReadOnly()
}

// File is an interface that needs to be fulfilled by any file implementation
// to be usable within the OCM Bindings.
// The File is a typical file implementation that is also writeable.
type File interface {
	fs.File
	io.Writer
}

func NewFS(base string, flag int) (*OSFileSystem, error) {
	base, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("unable to get absolute path: %w", err)
	}
	fi, err := os.Stat(base)
	if os.IsNotExist(err) {
		if flag&os.O_CREATE == 0 {
			return nil, fmt.Errorf("path does not exist: %s", base)
		}
		if err = os.MkdirAll(base, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create path: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("unable to stat path: %w", err)
	}
	// fi might be nil if we just create the directory in the os.IsNotExist
	// branch
	if fi != nil && !fi.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", base)
	}
	return &OSFileSystem{base: base, flag: flag}, nil
}

type OSFileSystem struct {
	// base is the base path of the filesystem
	base string
	// flagMu is a mutex to protect the flag read / write access
	flagMu sync.RWMutex
	// flag is the bitmask applied to limit fs operations with e.g. os.O_RDONLY
	// see os.OpenFile for details
	flag int
}

func (s *OSFileSystem) Base() string {
	return s.base
}

//nolint:wrapcheck // os.Remove should be propagated as is
func (s *OSFileSystem) Remove(name string) error {
	if s.ReadOnly() {
		return ErrReadOnly
	}
	return os.Remove(filepath.Join(s.base, name))
}

//nolint:wrapcheck // os.OpenFile should be propagated as is
func (s *OSFileSystem) OpenFile(name string, flag int, perm os.FileMode) (fs.File, error) {
	if s.ReadOnly() && !isFlagReadOnly(flag) {
		return nil, ErrReadOnly
	}
	return os.OpenFile(filepath.Join(s.base, name), flag, perm)
}

//nolint:wrapcheck // os.Open should be propagated as is
func (s *OSFileSystem) Open(name string) (fs.File, error) {
	return os.Open(filepath.Join(s.base, name))
}

//nolint:wrapcheck // os.ReadDir should be propagated as is
func (s *OSFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(filepath.Join(s.base, name))
}

//nolint:wrapcheck // os.MkdirAll should be propagated as is
func (s *OSFileSystem) MkdirAll(name string, perm os.FileMode) error {
	if s.ReadOnly() {
		return ErrReadOnly
	}
	return os.MkdirAll(filepath.Join(s.base, name), perm)
}

//nolint:wrapcheck // os.RemoveAll should be propagated as is
func (s *OSFileSystem) RemoveAll(path string) error {
	if s.ReadOnly() {
		return ErrReadOnly
	}
	return os.RemoveAll(filepath.Join(s.base, path))
}

//nolint:wrapcheck // os.Stat should be propagated as is
func (s *OSFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(filepath.Join(s.base, name))
}

func (s *OSFileSystem) ReadOnly() bool {
	s.flagMu.RLock()
	defer s.flagMu.RUnlock()
	return isFlagReadOnly(s.flag)
}

func (s *OSFileSystem) ForceReadOnly() {
	s.flagMu.Lock()
	defer s.flagMu.Unlock()
	s.flag &= os.O_RDONLY
}

// isFlagReadOnly checks if the flag is read only.
// It returns true if the flag is O_RDONLY or if the flag is not O_WRONLY or O_RDWR (because the default open mode
// is read only).
func isFlagReadOnly(flag int) bool {
	return flag&os.O_RDONLY != 0 || (flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0)
}
