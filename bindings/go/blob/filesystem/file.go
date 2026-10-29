package filesystem

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
)

// CopyBlobToOSPath copies the content of a blob.ReadOnlyBlob to a local path on the operating system's filesystem.
// The file is created with 0600 if it does not exist (os.O_CREATE) and truncated if it does (os.O_TRUNC).
// If reading or closing the blob fails, for example on a digest mismatch that a verifying blob reports at EOF
// or on Close, the written regular file is truncated to zero and path is removed if it is a regular file itself,
// so no rejected content is left behind. A symlink at path is kept and its target is left empty.
// Content already written to a named pipe or device cannot be retracted.
// The content is copied with blob.CopyReader.
func CopyBlobToOSPath(blob blob.ReadOnlyBlob, path string) error {
	data, err := blob.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get resource data: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to open target file %s: %w", path, err), data.Close())
	}
	// Close data before checking for errors: verifying readers may report a mismatch only on Close.
	if err := errors.Join(copyBuffered(file, data), data.Close()); err != nil {
		fi, statErr := file.Stat()
		regular := statErr == nil && fi.Mode().IsRegular()
		if regular {
			err = errors.Join(err, file.Truncate(0))
		}
		err = errors.Join(err, file.Close())
		if li, lstatErr := os.Lstat(path); regular && lstatErr == nil && li.Mode().IsRegular() {
			err = errors.Join(err, os.Remove(path))
		}
		return err
	}
	return file.Close()
}

func copyBuffered(dst io.Writer, src io.Reader) error {
	if _, err := blob.CopyReader(dst, src); err != nil {
		return fmt.Errorf("failed to copy resource data: %w", err)
	}
	return nil
}

// GetBlobFromOSPath returns a read-only blob for the file at path.
// A relative path is resolved against the current working directory once, on creation.
// The file is opened by path on every read and no descriptor is held between reads.
// Symlinks are followed like os.Open. A missing or unreadable file is reported when the blob is read.
func GetBlobFromOSPath(path string) (*Blob, error) {
	if path == "" {
		return nil, errors.New("path must not be empty")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	return NewFileBlob(osDirFS(filepath.Dir(path)), filepath.Base(path)), nil
}

// osDirFS is a read-only fs.FS over an absolute directory. Unlike os.DirFS it keeps
// the full path in errors, so a failed read names the file's directory.
type osDirFS string

var _ fs.StatFS = osDirFS("")

func (dir osDirFS) Open(name string) (fs.File, error) {
	// A nil *os.File must not be returned as a non-nil fs.File.
	f, err := os.Open(filepath.Join(string(dir), name))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (dir osDirFS) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(filepath.Join(string(dir), name))
}

// GetBlobInWorkingDirectory returns a read-only blob for a file that must stay inside workingDir.
// A relative path is resolved against workingDir, an absolute path must lie within it.
// Confinement is checked on creation and enforced again on every read with os.OpenInRoot,
// so a file swapped for an escaping symlink after creation cannot be read.
// Symlinks that stay inside workingDir are followed. No descriptor is held between reads.
func GetBlobInWorkingDirectory(path, workingDir string) (*Blob, error) {
	root, rel, err := resolveInWorkingDirectory(path, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure path in working directory: %w", err)
	}
	return NewFileBlob(workingDirFS(root), rel), nil
}

// EnsurePathInWorkingDirectory ensures that the given path is resolved against the specified working directory.
// If the path is absolute, it checks if the path is valid within the working directory.
// If the path is relative, it resolves it against the working directory and prevents escaping the working directory.
// If the path cannot be resolved or is invalid, it returns an error.
func EnsurePathInWorkingDirectory(path, workingDirectory string) (string, error) {
	root, rel, err := resolveInWorkingDirectory(path, workingDirectory)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, rel), nil
}

// resolveInWorkingDirectory returns the absolute working directory and path relative to it,
// after checking that path resolves without escaping the working directory.
// It stats instead of opening, so it neither blocks on a FIFO nor needs read permission.
func resolveInWorkingDirectory(path, workingDirectory string) (root, rel string, err error) {
	root, err = filepath.Abs(workingDirectory)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve working directory %q: %w", workingDirectory, err)
	}

	rel = path
	if filepath.IsAbs(path) {
		if rel, err = filepath.Rel(root, path); err != nil {
			return "", "", fmt.Errorf("failed to create relative path for %q based on working directory %q: %w", path, root, err)
		}
	}
	rel = filepath.Clean(rel)

	if _, err := workingDirFS(root).Stat(rel); err != nil {
		return "", "", fmt.Errorf("failed to stat path %q in root %q: %w", rel, root, err)
	}
	return root, rel, nil
}

// workingDirFS is a read-only fs.FS that confines every access to the directory
// by opening a fresh os.Root per call, so it holds no descriptor between calls.
type workingDirFS string

var _ fs.StatFS = workingDirFS("")

func (dir workingDirFS) Open(name string) (fs.File, error) {
	// A nil *os.File must not be returned as a non-nil fs.File.
	f, err := os.OpenInRoot(string(dir), name)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Stat does not open the file itself, so a named pipe without a writer does not block.
func (dir workingDirFS) Stat(name string) (fs.FileInfo, error) {
	root, err := os.OpenRoot(string(dir))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Stat(name)
}
