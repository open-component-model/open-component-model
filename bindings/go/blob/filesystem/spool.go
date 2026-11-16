package filesystem

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// spoolFilePrefix starts the name of every spool file, where it has one.
const spoolFilePrefix = "ocm-blob-"

// Spool copies content into a temporary file in dir and returns it as a [Blob] of known
// size and digest. Both are learned while the file is written, the digest with algorithm
// ([digest.Canonical] if empty), so content is read once and never held in memory. An empty
// dir stands for the default temporary directory of the operating system; a directory that
// does not exist is created.
//
// The file does not outlive the process, not even one that is killed: on unix it is unlinked
// as soon as it is created and never shows up in dir, on Windows it is deleted when it is
// closed. [Blob.Remove] frees it earlier.
//
// Every reader of the blob has its own position and can be rewound. The readers share the
// handle the blob was written with, so they fail once the blob is removed.
func Spool(content blob.ReadOnlyBlob, dir string, algorithm digest.Algorithm) (_ *Blob, err error) {
	if algorithm == "" {
		algorithm = digest.Canonical
	}
	if !algorithm.Available() {
		return nil, fmt.Errorf("digest algorithm %q is not available", algorithm)
	}
	if dir != "" {
		// Do not depend on someone else having created the directory.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create spool directory: %w", err)
		}
	}
	file, err := createSpoolFile(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create spool file: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()

	digester := algorithm.Digester()
	if err := copyAndClose(io.MultiWriter(file, digester.Hash()), content); err != nil {
		return nil, fmt.Errorf("failed to spool blob to file: %w", err)
	}

	name := filepath.Base(file.Name())
	spooled := NewFileBlob(spoolFS{file: file, name: name}, name)
	spooled.digest = digester.Digest()
	return spooled, nil
}

// copyAndClose copies src into dst. A source that verifies its content reports a mismatch
// when it is closed, so the close error is part of the result.
func copyAndClose(dst io.Writer, src blob.ReadOnlyBlob) (err error) {
	reader, err := src.ReadCloser()
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()
	_, err = blob.CopyReader(dst, reader)
	return err
}

// spoolFS is the file system of a spooled blob. It holds the one file of the blob, reached
// through the handle it was written with because the file may have no name. Removing the
// file closes that handle, which is what frees it.
type spoolFS struct {
	file *os.File
	name string
}

var (
	_ fs.StatFS = spoolFS{}
	_ RemoveFS  = spoolFS{}
)

func (f spoolFS) Open(name string) (fs.File, error) {
	if name != f.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	// A file without a name cannot be opened a second time, and one that Windows deletes on
	// close only by a handle that shares delete access. Every reader therefore goes through
	// the one handle, at its own offset.
	info, err := f.file.Stat()
	if err != nil {
		return nil, err
	}
	return sharedSpoolFile{SectionReader: io.NewSectionReader(f.file, 0, info.Size()), file: f.file}, nil
}

func (f spoolFS) Stat(name string) (fs.FileInfo, error) {
	if name != f.name {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return f.file.Stat()
}

func (f spoolFS) Remove(name string) error {
	if name != f.name {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	return f.file.Close()
}

// sharedSpoolFile reads a spool file at its own position through a handle it shares, so
// closing it leaves that handle open. Seek and ReadAt stay visible: HTTP clients look for
// them to tell that a request body can be sent again after a failed attempt.
type sharedSpoolFile struct {
	*io.SectionReader
	file *os.File
}

func (f sharedSpoolFile) Stat() (fs.FileInfo, error) { return f.file.Stat() }

func (sharedSpoolFile) Close() error { return nil }
