package inmemory

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

var (
	// ErrNotExist is returned when attempting to access a file that does not exist
	ErrNotExist = fs.ErrNotExist
	// ErrIsDir is returned when attempting to perform file operations on a directory
	ErrIsDir = errors.New("is a directory")
	// ErrIsFile is returned when attempting to perform directory operations on a file
	ErrIsFile = errors.New("is a file")
)

var (
	_ filesystem.FileSystem = (*FileSystem)(nil)
	_ fs.FS                 = (*FileSystem)(nil)
	_ fs.ReadDirFS          = (*FileSystem)(nil)
)

// FileSystem implements the [filesystem.FileSystem] interface using in-memory storage.
// It provides a thread-safe implementation of a virtual filesystem that stores all
// files and directories in memory.
//
// FileSystem attempts to model the unix file model closely and attempts to provide best-effort
// compatibility with the [os.File] interface. It is not a complete reenactment
// but can deal with file permissions, nested structures and file content.
type FileSystem struct {
	// root represents the root directory of the filesystem
	root *node
	// readOnly indicates whether the filesystem is in read-only mode
	readOnly bool
	// mu provides thread-safe access to the filesystem
	mu sync.RWMutex
}

// node represents a file or directory in the in-memory filesystem.
// It contains all metadata and content for a single filesystem entry.
type node struct {
	// name is the name of the file or directory
	name string
	// isDir indicates whether this node represents a directory
	isDir bool
	// content stores the actual file content for regular files
	content []byte
	// parent points to the parent node of this node, if present
	parent *node
	// children stores child nodes for directories
	children map[string]*node
	// modTime records the last modification time
	modTime time.Time
	// mode stores the file permissions and type
	mode os.FileMode
	// refCount tracks the number of open file descriptors
	refCount int32
	// deleted indicates if the file has been marked for deletion
	deleted bool
	// mu provides thread-safe access to the node
	mu sync.Mutex
}

// New creates a new in-memory filesystem
func New() *FileSystem {
	return &FileSystem{
		root: &node{
			name:     "/",
			isDir:    true,
			children: make(map[string]*node),
			modTime:  time.Now(),
			mode:     os.ModeDir | 0o755,
		},
	}
}

// Base returns the base path of the filesystem
func (inmemory *FileSystem) Base() string {
	return "inmemory"
}

// Open opens a file for reading
func (inmemory *FileSystem) Open(name string) (fs.File, error) {
	inmemory.mu.RLock()
	defer inmemory.mu.RUnlock()

	node, err := inmemory.getNode(name)
	if err != nil {
		return nil, err
	}

	// Check read permissions
	if node.mode&0o400 == 0 {
		return nil, os.ErrPermission
	}

	node.mu.Lock()
	// Check if file is deleted and has no open references
	if node.deleted {
		node.mu.Unlock()
		return nil, ErrNotExist
	}
	node.refCount++
	node.mu.Unlock()

	if node.isDir {
		return &dirFile{node: node, fs: inmemory}, nil
	}
	return &file{node: node, fs: inmemory}, nil
}

// updateParentModTimes updates the modification time of all parent directories up to root
func (inmemory *FileSystem) updateParentModTimes(node *node) {
	// Get the full path of the node
	current := node
	for current.parent != nil {
		current.parent.modTime = node.modTime
		current = current.parent
	}
}

// OpenFile opens a file with the specified flags and permissions
func (inmemory *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (fs.File, error) {
	if inmemory.readOnly {
		return nil, errors.New("filesystem is read-only")
	}

	inmemory.mu.Lock()
	n, err := inmemory.getNode(name)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			if flag&os.O_CREATE == 0 {
				inmemory.mu.Unlock()
				return nil, err
			}

			// Get parent node
			var parent *node
			parentDir := filepath.Dir(name)
			if parentDir == "." || parentDir == "/" {
				parent = inmemory.root
			} else {
				// Release lock before creating parent directories
				inmemory.mu.Unlock()
				if err := inmemory.MkdirAll(parentDir, 0o755); err != nil {
					return nil, err
				}
				// Reacquire lock
				inmemory.mu.Lock()
				parent, err = inmemory.getNode(parentDir)
				if err != nil {
					inmemory.mu.Unlock()
					return nil, err
				}
			}

			// Check parent directory write permissions
			if parent.mode&0o200 == 0 {
				inmemory.mu.Unlock()
				return nil, os.ErrPermission
			}

			// Create new file
			n = &node{
				name:     filepath.Base(name),
				isDir:    false,
				content:  []byte{},
				modTime:  time.Now(),
				mode:     perm,
				refCount: 1,
				deleted:  false,
				parent:   parent,
			}
			parent.children[n.name] = n
			// Update all parent directories' modification times
			inmemory.updateParentModTimes(n)
		} else {
			inmemory.mu.Unlock()
			return nil, err
		}
	} else {
		// Check if the requested access mode is allowed by the file's current permissions
		if flag&os.O_RDONLY != 0 && n.mode&0o400 == 0 {
			inmemory.mu.Unlock()
			return nil, os.ErrPermission
		}
		if (flag&os.O_WRONLY != 0 || flag&os.O_RDWR != 0) && n.mode&0o200 == 0 {
			inmemory.mu.Unlock()
			return nil, os.ErrPermission
		}

		n.mu.Lock()
		// Check if file is deleted and has no open references
		if n.deleted {
			n.mu.Unlock()
			inmemory.mu.Unlock()
			return nil, ErrNotExist
		}
		n.refCount++
		n.mu.Unlock()
	}

	if n.isDir {
		inmemory.mu.Unlock()
		return nil, ErrIsDir
	}

	file := &file{node: n, fs: inmemory}
	inmemory.mu.Unlock()
	return file, nil
}

// MkdirAll creates a directory and all necessary parent directories
func (inmemory *FileSystem) MkdirAll(name string, perm os.FileMode) error {
	if inmemory.readOnly {
		return errors.New("filesystem is read-only")
	}

	inmemory.mu.Lock()
	defer inmemory.mu.Unlock()

	parts := strings.Split(strings.Trim(name, "/"), "/")
	current := inmemory.root

	for _, part := range parts {
		if part == "" {
			continue
		}

		if child, exists := current.children[part]; exists {
			if !child.isDir {
				return ErrIsFile
			}
			current = child
		} else {
			newNode := &node{
				name:     part,
				isDir:    true,
				children: make(map[string]*node),
				modTime:  time.Now(),
				mode:     perm | os.ModeDir,
			}
			current.children[part] = newNode
			current = newNode
		}
	}

	return nil
}

// Remove removes a file or empty directory
func (inmemory *FileSystem) Remove(name string) error {
	if inmemory.readOnly {
		return errors.New("filesystem is read-only")
	}

	// Prevent removing root directory
	if name == "/" {
		return errors.New("cannot remove root directory")
	}

	inmemory.mu.Lock()
	defer inmemory.mu.Unlock()

	parentDir := filepath.Dir(name)
	base := filepath.Base(name)

	// If parent is "." or "/", use root as parent
	var parent *node
	if parentDir == "." || parentDir == "/" {
		parent = inmemory.root
	} else {
		var err error
		parent, err = inmemory.getNode(parentDir)
		if err != nil {
			return err
		}
	}

	node, exists := parent.children[base]
	if !exists {
		return ErrNotExist
	}

	// Allow removing empty directories
	if node.isDir && len(node.children) > 0 {
		return errors.New("directory not empty")
	}

	node.mu.Lock()
	if node.refCount > 0 {
		// Mark as deleted but keep accessible through open file descriptors
		node.deleted = true
		node.mu.Unlock()
		return nil
	}
	node.mu.Unlock()

	delete(parent.children, base)
	return nil
}

// ReadDir reads the directory entries
func (inmemory *FileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	inmemory.mu.RLock()
	defer inmemory.mu.RUnlock()

	node, err := inmemory.getNode(name)
	if err != nil {
		return nil, err
	}

	if !node.isDir {
		return nil, ErrIsFile
	}

	// Check directory read permissions
	if node.mode&0o400 == 0 {
		return nil, os.ErrPermission
	}

	entries := make([]fs.DirEntry, 0, len(node.children))
	for _, child := range node.children {
		entries = append(entries, &DirEntry{node: child})
	}

	return entries, nil
}

// RemoveAll removes a file or directory and all its contents
func (inmemory *FileSystem) RemoveAll(path string) error {
	if inmemory.readOnly {
		return errors.New("filesystem is read-only")
	}

	// Prevent removing root directory
	if path == "/" {
		return errors.New("cannot remove root directory")
	}

	inmemory.mu.Lock()
	defer inmemory.mu.Unlock()

	parentDir := filepath.Dir(path)
	base := filepath.Base(path)

	// If parent is "." or "/", use root as parent
	var parent *node
	if parentDir == "." || parentDir == "/" {
		parent = inmemory.root
	} else {
		var err error
		parent, err = inmemory.getNode(parentDir)
		if err != nil {
			return err
		}
	}

	node, exists := parent.children[base]
	if !exists {
		return ErrNotExist
	}

	node.mu.Lock()
	if node.refCount > 0 {
		// Mark as deleted but keep accessible through open file descriptors
		node.deleted = true
		node.mu.Unlock()
		return nil
	}
	node.mu.Unlock()

	delete(parent.children, base)
	return nil
}

// Stat returns file information
func (inmemory *FileSystem) Stat(name string) (fs.FileInfo, error) {
	inmemory.mu.RLock()
	defer inmemory.mu.RUnlock()

	node, err := inmemory.getNode(name)
	if err != nil {
		return nil, err
	}

	return &FileInfo{node: node}, nil
}

// ReadOnly returns whether the filesystem is read-only
func (inmemory *FileSystem) ReadOnly() bool {
	return inmemory.readOnly
}

// ForceReadOnly makes the filesystem read-only
func (inmemory *FileSystem) ForceReadOnly() {
	inmemory.readOnly = true
}

// getNode retrieves a node from the filesystem
func (inmemory *FileSystem) getNode(name string) (*node, error) {
	if name == "/" {
		return inmemory.root, nil
	}

	parts := strings.Split(strings.Trim(name, "/"), "/")
	current := inmemory.root

	for _, part := range parts {
		if part == "" {
			continue
		}

		child, exists := current.children[part]
		if !exists {
			return nil, ErrNotExist
		}
		current = child
	}

	return current, nil
}

// file implements fs.File and io.Writer for regular files.
// It provides read/write access to file content with position tracking.
type file struct {
	// node points to the underlying filesystem node
	node *node
	// fs references the parent filesystem
	fs *FileSystem
	// pos tracks the current read/write position
	pos int64
	// closed indicates whether the file has been closed
	closed bool
}

// Ensure file implements io.Writer and io.Seeker
var (
	_ io.Writer = (*file)(nil)
	_ io.Seeker = (*file)(nil)
)

func (f *file) Read(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if f.node.mode&0o400 == 0 {
		return 0, os.ErrPermission
	}
	if f.pos >= int64(len(f.node.content)) {
		return 0, io.EOF
	}
	n = copy(p, f.node.content[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *file) Write(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if f.fs.readOnly {
		return 0, filesystem.ErrReadOnly
	}

	// Check write permissions
	if f.node.mode&0o200 == 0 {
		return 0, os.ErrPermission
	}

	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()

	if f.pos == int64(len(f.node.content)) {
		f.node.content = append(f.node.content, p...)
	} else {
		f.node.content = append(f.node.content[:f.pos], p...)
	}
	f.pos += int64(len(p))
	f.node.modTime = time.Now()
	// Update all parent directories' modification times when file is modified
	f.fs.updateParentModTimes(f.node)
	return len(p), nil
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, os.ErrClosed
	}

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = f.pos + offset
	case io.SeekEnd:
		newPos = int64(len(f.node.content)) + offset
	default:
		return 0, fmt.Errorf("invalid whence %d: %w", whence, os.ErrInvalid)
	}

	if newPos < 0 {
		return 0, fmt.Errorf("invalid offset %d: %w", offset, os.ErrInvalid)
	}

	f.pos = newPos
	return f.pos, nil
}

func (f *file) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	f.node.mu.Lock()
	f.node.refCount--
	if f.node.deleted && f.node.refCount == 0 {
		// If the file was marked as deleted and this was the last reference,
		// remove it from the parent's children
		parentDir := filepath.Dir(f.node.name)
		base := filepath.Base(f.node.name)
		var parent *node
		if parentDir == "." || parentDir == "/" {
			parent = f.fs.root
			delete(parent.children, base)
		} else {
			var err error
			parent, err = f.fs.getNode(parentDir)
			if err == nil {
				delete(parent.children, base)
			}
		}
	}
	f.node.mu.Unlock()
	return nil
}

func (f *file) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, os.ErrClosed
	}
	return &FileInfo{node: f.node}, nil
}

// dirFile implements fs.File for directories.
// It provides directory listing functionality.
type dirFile struct {
	// node points to the underlying filesystem node
	node *node
	// fs references the parent filesystem
	fs *FileSystem
	// pos tracks the current position in directory listing
	pos int
	// closed indicates whether the directory has been closed
	closed bool
}

func (d *dirFile) Read(p []byte) (n int, err error) {
	if d.closed {
		return 0, os.ErrClosed
	}
	return 0, ErrIsDir
}

func (d *dirFile) Write(p []byte) (n int, err error) {
	if d.closed {
		return 0, os.ErrClosed
	}
	return 0, ErrIsDir
}

func (d *dirFile) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.node.mu.Lock()
	d.node.refCount--
	if d.node.deleted && d.node.refCount == 0 {
		// If the directory was marked as deleted and this was the last reference,
		// remove it from the parent's children
		parentDir := filepath.Dir(d.node.name)
		base := filepath.Base(d.node.name)
		var parent *node
		if parentDir == "." || parentDir == "/" {
			parent = d.fs.root
			delete(parent.children, base)
		} else {
			var err error
			parent, err = d.fs.getNode(parentDir)
			if err == nil {
				delete(parent.children, base)
			}
		}
	}
	d.node.mu.Unlock()
	return nil
}

func (d *dirFile) Stat() (fs.FileInfo, error) {
	if d.closed {
		return nil, os.ErrClosed
	}
	return &FileInfo{node: d.node}, nil
}

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.closed {
		return nil, os.ErrClosed
	}

	entries := make([]fs.DirEntry, 0, len(d.node.children))
	for _, child := range d.node.children {
		entries = append(entries, &DirEntry{node: child})
	}

	if n <= 0 {
		return entries, nil
	}

	if d.pos >= len(entries) {
		return nil, io.EOF
	}

	end := d.pos + n
	if end > len(entries) {
		end = len(entries)
	}

	result := entries[d.pos:end]
	d.pos = end
	return result, nil
}

// FileInfo implements fs.FileInfo interface.
// It provides metadata about a file or directory.
type FileInfo struct {
	// node points to the underlying filesystem node
	node *node
}

func (fi *FileInfo) Name() string {
	return fi.node.name
}

func (fi *FileInfo) Size() int64 {
	if fi.node.isDir {
		return 0
	}
	return int64(len(fi.node.content))
}

func (fi *FileInfo) Mode() fs.FileMode {
	return fi.node.mode
}

func (fi *FileInfo) ModTime() time.Time {
	return fi.node.modTime
}

func (fi *FileInfo) IsDir() bool {
	return fi.node.isDir
}

func (fi *FileInfo) Sys() interface{} {
	return fi.node
}

// DirEntry implements fs.DirEntry interface.
// It provides information about a single directory entry.
type DirEntry struct {
	// node points to the underlying filesystem node
	node *node
}

func (de *DirEntry) Name() string {
	return de.node.name
}

func (de *DirEntry) IsDir() bool {
	return de.node.isDir
}

func (de *DirEntry) Type() fs.FileMode {
	return de.node.mode
}

func (de *DirEntry) Info() (fs.FileInfo, error) {
	return &FileInfo{node: de.node}, nil
}
