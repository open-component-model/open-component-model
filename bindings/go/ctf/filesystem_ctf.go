package ctf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

const (
	BlobsDirectoryName = "blobs"
)

// FileSystemCTF is a CTF implementation that uses any filesystem.FileSystem as the underlying storage.
// It is used to read and write CTFs from a directory structure.
// This is the canonical implementation of the CTF interface, accessing
//   - the index file at v1.ArtifactIndexFileName
//   - the blobs at BlobsDirectoryName
//
// The CTF offered will always be of type FormatDirectory.
//
// Concurrency: All file writes use atomic operations (temp file + rename) to prevent
// race conditions and ensure readers never see partially written files during concurrent access.
type FileSystemCTF struct {
	fs        fs.FS
	statFS    fs.StatFS
	readDirFS fs.ReadDirFS
	mkdirFS   filesystem.MkdirAllFS
	ofFS      filesystem.OpenFileFS
	remFS     filesystem.RemoveFS
}

var (
	_ CTF                = (*FileSystemCTF)(nil)
	_ StreamingBlobStore = (*FileSystemCTF)(nil)
)

// NewFileSystemCTF opens a CTF with the specified filesystem as its root
func NewFileSystemCTF(fsys fs.FS) *FileSystemCTF {
	base := &FileSystemCTF{
		fs: fsys,
	}
	if statFS, ok := fsys.(fs.StatFS); ok {
		base.statFS = statFS
	}
	if mkdirFS, ok := fsys.(filesystem.MkdirAllFS); ok {
		base.mkdirFS = mkdirFS
	}
	if readDirFS, ok := fsys.(fs.ReadDirFS); ok {
		base.readDirFS = readDirFS
	}
	if ofFS, ok := fsys.(filesystem.OpenFileFS); ok {
		base.ofFS = ofFS
	}
	if remFS, ok := fsys.(filesystem.RemoveFS); ok {
		base.remFS = remFS
	}
	return base
}

// FS returns the underlying filesystem.FileSystem of the CTF.
// Note that write operations to the FileSystem can affect the integrity of the CTF.
// TODO(jakobmoellerdev): restrict returned FileSystem to only allow read operations.
func (c *FileSystemCTF) FS() fs.FS {
	return c.fs
}

// Format always returns FormatDirectory for FileSystemCTF.
func (c *FileSystemCTF) Format() FileFormat {
	return FormatDirectory
}

// GetIndex returns the v1.ArtifactIndexFileName parsed as v1.Index of the CTF.
// If the CTF is empty, an empty index is returned so it can be set with SetIndex.
func (c *FileSystemCTF) GetIndex(_ context.Context) (index v1.Index, err error) {
	if c.statFS == nil {
		return nil, fmt.Errorf("index cannot be retrieved from a filesystem that does not support stat: %T", c.fs)
	}

	fi, err := c.statFS.Stat(v1.ArtifactIndexFileName)

	if errors.Is(err, fs.ErrNotExist) {
		return v1.NewIndex(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to stat %s: %w", v1.ArtifactIndexFileName, err)
	}

	if fi.Size() == 0 {
		return v1.NewIndex(), nil
	}

	var indexFile fs.File
	if indexFile, err = c.fs.Open(v1.ArtifactIndexFileName); err != nil {
		return nil, fmt.Errorf("unable to open artifact index: %w", err)
	}
	defer func() {
		err = errors.Join(err, indexFile.Close())
	}()

	if index, err = v1.DecodeIndex(indexFile); err != nil {
		return nil, fmt.Errorf("unable to decode artifact index: %w", err)
	}

	return index, nil
}

// SetIndex sets the v1.ArtifactIndexFileName of the CTF to the given index.
func (c *FileSystemCTF) SetIndex(_ context.Context, index v1.Index) (err error) {
	data, err := v1.Encode(index)
	if err != nil {
		return fmt.Errorf("unable to encode artifact index: %w", err)
	}

	return c.writeFile(v1.ArtifactIndexFileName, bytes.NewReader(data), int64(len(data)), nil)
}

// ioBufPool is a pool of byte buffers that can be reused for copying content
// between i/o relevant data, such as files.
var ioBufPool = sync.Pool{
	New: func() interface{} {
		// the buffer size should be larger than or equal to 128 KiB
		// for performance considerations.
		// we choose 1 MiB here so there will be less disk I/O.
		buffer := make([]byte, blob.DefaultArchiveBlobBufferSize)
		return &buffer
	},
}

// writeFile writes the given raw data to the given name in the CTF.
// If the directory does not exist, it will be created.
// Always uses atomic write (temp file + rename, see stageAndPublish) for safe concurrent access.
// If beforeCommit is set, it runs after the content is staged and before it replaces name;
// if it fails, only the staged content is removed and name stays untouched.
func (c *FileSystemCTF) writeFile(name string, raw io.Reader, size int64, beforeCommit func() error) error {
	return c.stageAndPublish(filepath.Dir(name), "."+filepath.Base(name)+".*", raw, size, func() (string, error) {
		if beforeCommit != nil {
			if err := beforeCommit(); err != nil {
				return "", err
			}
		}
		return name, nil
	})
}

// stageAndPublish stages raw in a temporary file in dir (relative to the CTF root) and
// atomically renames it to the name returned by commit, which must lie in dir.
// commit runs after the content is staged and the temp file is closed; if it fails,
// the staged file is removed. An empty name with a nil error discards the staged file
// and publishes nothing (the content is already stored).
func (c *FileSystemCTF) stageAndPublish(dir, tempPattern string, raw io.Reader, size int64, commit func() (string, error)) (err error) {
	// Ensure directory exists
	if c.mkdirFS != nil {
		if err := c.mkdirFS.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("unable to create directory: %w", err)
		}
	}

	// Check if this is an OS filesystem
	osFS, ok := c.fs.(*filesystem.RootFileSystem)
	if !ok {
		return fmt.Errorf("atomic writes not supported on non-OS filesystem: %T", c.fs)
	}

	// Create temp file
	tempFile, err := os.CreateTemp(filepath.Join(osFS.String(), dir), tempPattern)
	if err != nil {
		return fmt.Errorf("unable to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Write content to temp file
	if size <= blob.SizeUnknown {
		buf := ioBufPool.Get().(*[]byte)
		defer ioBufPool.Put(buf)
		if _, err = io.CopyBuffer(tempFile, raw, *buf); err != nil {
			err = errors.Join(err, tempFile.Close(), os.Remove(tempPath))
			return fmt.Errorf("unable to write content: %w", errors.Join(err, tempFile.Close(), os.Remove(tempPath)))
		}
	} else {
		if _, err = io.CopyN(tempFile, raw, size); err != nil {
			err = errors.Join(err, tempFile.Close(), os.Remove(tempPath))
			return fmt.Errorf("unable to write content: %w", err)
		}
	}

	// Close file before chmod and rename (required for Windows compatibility)
	if err = tempFile.Close(); err != nil {
		err = errors.Join(err, os.Remove(tempPath))
		return fmt.Errorf("unable to close temp file: %w", err)
	}

	// Ensure cleanup on error after this point
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tempPath))
		}
	}()

	var name string
	if name, err = commit(); err != nil {
		return err
	}
	if name == "" {
		// The content is already stored under its name; drop the staged copy.
		return os.Remove(tempPath)
	}

	// Make read-only
	if err = os.Chmod(tempPath, 0o444); err != nil {
		return fmt.Errorf("unable to make temp file readonly: %w", err)
	}

	// Atomic rename
	if err = os.Rename(tempPath, filepath.Join(osFS.String(), name)); err != nil {
		return fmt.Errorf("unable to rename temp file: %w", err)
	}

	return nil
}

// DeleteBlob deletes the blob with the given digest from the CTF by removing the file from BlobsDirectoryName.
func (c *FileSystemCTF) DeleteBlob(_ context.Context, digest string) (err error) {
	if c.remFS == nil {
		return fmt.Errorf("filesystem does not support removing files: %T", c.fs)
	}

	file, err := ToBlobFileName(digest)
	if err != nil {
		return err
	}
	if err = c.remFS.Remove(filepath.Join(BlobsDirectoryName, file)); err != nil {
		return fmt.Errorf("unable to delete blob: %w", err)
	}

	return nil
}

// GetBlob returns the blob with the given digest from the CTF by reading the file from BlobsDirectoryName.
func (c *FileSystemCTF) GetBlob(_ context.Context, digest string) (blob.ReadOnlyBlob, error) {
	if c.statFS == nil {
		return nil, fmt.Errorf("filesystem does not support stat: %T", c.fs)
	}

	file, err := ToBlobFileName(digest)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(BlobsDirectoryName, file)
	if _, err := c.statFS.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("blob %s not found: %w", digest, err)
		}
		return nil, fmt.Errorf("unable to stat blob: %w", err)
	}

	b := NewCASFileBlob(c.fs, filepath.Join(BlobsDirectoryName, file))
	b.SetPrecalculatedDigest(digest)
	return b, nil
}

// ListBlobs returns a list of all blobs in the CTF by listing the files in BlobsDirectoryName.
func (c *FileSystemCTF) ListBlobs(_ context.Context) (digests []string, err error) {
	if c.readDirFS == nil {
		return nil, fmt.Errorf("filesystem does not support reading directories: %T", c.fs)
	}

	dir, err := c.readDirFS.ReadDir(BlobsDirectoryName)
	if err != nil {
		return nil, fmt.Errorf("unable to list blobs: %w", err)
	}

	digests = make([]string, 0, len(dir))
	for _, entry := range dir {
		if entry.Type().IsRegular() {
			digests = append(digests, ToDigest(entry.Name()))
		}
	}

	return digests, nil
}

func (c *FileSystemCTF) SaveBlob(ctx context.Context, b blob.ReadOnlyBlob) (err error) {
	digestable, ok := b.(blob.DigestAware)
	if !ok {
		return errors.New("blob does not have a digest that can be used to save it")
	}

	dig, known := digestable.Digest()
	if !known {
		return errors.New("blob does not have a digest that can be used to save it")
	}

	file, err := ToBlobFileName(dig)
	if err != nil {
		return err
	}
	path := filepath.Join(BlobsDirectoryName, file)

	// Blobs are content-addressed, so an existing blob already holds this content and is
	// kept. The content is still read and verified, as callers rely on it being consumed.
	// Not replacing it also means a blob is never renamed over while a reader may hold it
	// open: readers do not lock against writers for the whole read, and renaming over an
	// open file fails on Windows.
	exists := c.fileExists(path)

	size := blob.SizeUnknown
	if sizeable, ok := b.(blob.SizeAware); ok {
		size = sizeable.Size()
	}

	data, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("unable to read blob: %w", err)
	}
	// Closing data can fail when the content does not match its digest (e.g. a verifying
	// reader). It is closed before the staged content is published, so unverified content
	// is never stored under its digest.
	closed := false
	closeData := func() error {
		closed = true
		return data.Close()
	}
	defer func() {
		if !closed {
			err = errors.Join(err, data.Close())
		}
	}()

	ctxRead, err := newCtxReader(ctx, data)
	if err != nil {
		return fmt.Errorf("unable to create context reader: %w", err)
	}

	if exists {
		if _, err := io.Copy(io.Discard, ctxRead); err != nil {
			return fmt.Errorf("unable to read content: %w", err)
		}
		return closeData()
	}
	return c.writeFile(path, ctxRead, size, closeData)
}

// countingWriter counts the bytes written to it.
type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// SaveBlobStreaming stores data under its content digest, computing digest and size while
// reading. The content is staged in a temporary file in BlobsDirectoryName and renamed to
// its content address only after the digest and size are computed and verified; an already
// stored blob is never replaced.
func (c *FileSystemCTF) SaveBlobStreaming(ctx context.Context, data io.Reader, expectedDigest digest.Digest, expectedSize int64) (digest.Digest, int64, error) {
	algorithm := digest.Canonical
	if expectedDigest != "" {
		if err := expectedDigest.Validate(); err != nil {
			return "", 0, fmt.Errorf("invalid expected digest %q: %w", expectedDigest, err)
		}
		algorithm = expectedDigest.Algorithm()
	}
	ctxRead, err := newCtxReader(ctx, data)
	if err != nil {
		return "", 0, fmt.Errorf("unable to create context reader: %w", err)
	}
	digester := algorithm.Digester()
	var counter countingWriter
	content := io.TeeReader(ctxRead, io.MultiWriter(digester.Hash(), &counter))

	// verify checks the consumed content and returns its digest and blob path.
	verify := func() (digest.Digest, string, error) {
		dig := digester.Digest()
		if expectedDigest != "" && dig != expectedDigest {
			return "", "", fmt.Errorf("content digest %s does not match expected digest %s", dig, expectedDigest)
		}
		if expectedSize >= 0 && counter.n != expectedSize {
			return "", "", fmt.Errorf("content size %d does not match expected size %d", counter.n, expectedSize)
		}
		file, err := ToBlobFileName(dig.String())
		if err != nil {
			return "", "", err
		}
		return dig, filepath.Join(BlobsDirectoryName, file), nil
	}

	// As in SaveBlob, an existing content-addressed blob is kept: only verify the content.
	if expectedDigest != "" {
		file, err := ToBlobFileName(expectedDigest.String())
		if err != nil {
			return "", 0, err
		}
		if c.fileExists(filepath.Join(BlobsDirectoryName, file)) {
			if _, err := io.Copy(io.Discard, content); err != nil {
				return "", 0, fmt.Errorf("unable to read content: %w", err)
			}
			dig, _, err := verify()
			if err != nil {
				return "", 0, err
			}
			return dig, counter.n, nil
		}
	}

	// The size is always passed as unknown so the stream is read to EOF: a known size
	// would make the copy stop early and silently truncate an over-long stream.
	var dig digest.Digest
	if err := c.stageAndPublish(BlobsDirectoryName, ".upload.*", content, blob.SizeUnknown, func() (string, error) {
		var path string
		var err error
		if dig, path, err = verify(); err != nil {
			return "", err
		}
		if c.fileExists(path) {
			return "", nil
		}
		return path, nil
	}); err != nil {
		return "", 0, err
	}
	return dig, counter.n, nil
}

// fileExists reports whether name exists; it is false when the filesystem cannot stat.
func (c *FileSystemCTF) fileExists(name string) bool {
	if c.statFS == nil {
		return false
	}
	_, err := c.statFS.Stat(name)
	return err == nil
}

// ToBlobFileName converts a digest to a blob file name by replacing the ":" with ".", which is the
// default separator for blobs in the CTF under BlobsDirectoryName.
func ToBlobFileName(dig string) (string, error) {
	// parse the digest to check if it is valid, ensure there are no invalid replacements
	if _, err := digest.Parse(dig); err != nil {
		return "", fmt.Errorf("invalid digest %q could not be converted to blob file name: %w", dig, err)
	}
	return strings.ReplaceAll(dig, ":", "."), nil
}

// ToDigest converts a blob file name to a digest by replacing the "." with ":", which is the
// default separator for digests in standard notation.
func ToDigest(blobFileName string) string {
	return strings.ReplaceAll(blobFileName, ".", ":")
}
