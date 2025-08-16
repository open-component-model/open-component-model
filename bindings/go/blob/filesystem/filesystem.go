package filesystem

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

var ErrReadOnly = fmt.Errorf("read only file system")

// FileSystem is an interface that needs to be fulfilled by any filesystem implementation
// to be usable within the OCM Bindings.
// RootFileSystem which is backed by the os package.
type FileSystem interface {
	String() string

	fs.FS
	fs.StatFS
	fs.ReadDirFS

	MkdirAllFS
	OpenFileFS
	RemoveFS
	RemoveAllFS
	ReadOnlyFS
}

// OpenFileFS is a filesystem that supports opening files with a specific flag and permission bitmask
type OpenFileFS interface {
	// OpenFile is the generalized open call; most users will use Open
	// or Create instead. It opens the named file with specified flag
	// (O_RDONLY etc.). If the file does not exist, and the O_CREATE flag
	// is passed, it is created with mode perm (before umask);
	// the containing directory must exist. If successful,
	// methods on the returned File can be used for I/O.
	OpenFile(name string, flag int, perm os.FileMode) (fs.File, error)
}

// RemoveFS is a filesystem that supports Remove of a file
type RemoveFS interface {
	// Remove removes the named file or (empty) directory.
	Remove(name string) error
}

// RemoveAllFS is a filesystem that supports RemoveAll of a directory
type RemoveAllFS interface {
	// RemoveAll removes path and any children it contains.
	// It removes everything it can but returns the first error it encounters.
	// If the path does not exist, RemoveAll returns nil (no error).
	RemoveAll(path string) error
}

type MkdirAllFS interface {
	// MkdirAll creates a directory named path, along with any necessary parents,
	// and returns nil, or else returns an error.
	// The permission bits perm (before umask) are used for all directories that MkdirAll creates.
	// If path is already a directory, MkdirAll does nothing and returns nil.
	MkdirAll(name string, perm os.FileMode) error
}

type ReadOnlyFS interface {
	// ReadOnly returns true if the filesystem is read only.
	ReadOnly() bool
	// ForceReadOnly sets the filesystem to read only mode, restricting all future operations.
	ForceReadOnly()
}

// File is an interface that needs to be fulfilled by any file implementation
// to be usable within the OCM Bindings.
// The File is a typical file implementation that is also writeable.
type File interface {
	fs.File
	io.Writer
}

func NewFS(base string, flag int) (*RootFileSystem, error) {
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
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("unable to open root on base: %w", err)
	}
	return &RootFileSystem{root: r, flag: flag}, nil
}

type RootFileSystem struct {
	// root may be used to only access files within a single directory tree.
	root *os.Root
	// flagMu is a mutex to protect the flag read / write access
	flagMu sync.RWMutex
	// flag is the bitmask applied to limit fs operations with e.g. os.O_RDONLY
	// see os.OpenFile for details
	flag int
}

func (s *RootFileSystem) String() string {
	return s.root.Name()
}

func (s *RootFileSystem) Remove(name string) error {
	if s.ReadOnly() {
		return ErrReadOnly
	}
	return s.root.Remove(name)
}

func (s *RootFileSystem) OpenFile(name string, flag int, perm os.FileMode) (fs.File, error) {
	if s.ReadOnly() && !isFlagReadOnly(flag) {
		return nil, ErrReadOnly
	}
	return s.root.OpenFile(name, flag, perm)
}

func (s *RootFileSystem) Open(name string) (fs.File, error) {
	return s.OpenFile(name, os.O_RDONLY, 0)
}

func (s *RootFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return s.root.FS().(fs.ReadDirFS).ReadDir(name)
}

func (s *RootFileSystem) MkdirAll(name string, perm os.FileMode) error {
	if s.ReadOnly() {
		return ErrReadOnly
	}
	return s.root.MkdirAll(name, perm)
}

func (s *RootFileSystem) RemoveAll(path string) error {
	if s.ReadOnly() {
		return ErrReadOnly
	}
	return s.root.RemoveAll(path)
}

func (s *RootFileSystem) Stat(name string) (fs.FileInfo, error) {
	return s.root.Stat(name)
}

func (s *RootFileSystem) ReadOnly() bool {
	s.flagMu.RLock()
	defer s.flagMu.RUnlock()
	return isFlagReadOnly(s.flag)
}

func (s *RootFileSystem) ForceReadOnly() {
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
