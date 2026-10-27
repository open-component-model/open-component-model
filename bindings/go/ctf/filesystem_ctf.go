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

var _ CTF = (*FileSystemCTF)(nil)

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

	return c.writeFile(v1.ArtifactIndexFileName, bytes.NewReader(data), int64(len(data)))
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
// Always uses atomic write (temp file + rename) for safe concurrent access.
func (c *FileSystemCTF) writeFile(name string, raw io.Reader, size int64) error {
	staged, err := c.stageFile(filepath.Dir(name), "."+filepath.Base(name)+".*", raw, size)
	if err != nil {
		return err
	}
	if err := publishFile(staged, filepath.Base(name)); err != nil {
		return errors.Join(err, os.Remove(staged))
	}
	return nil
}

// stageFile writes raw to a new temporary file in dir (relative to the CTF root) and
// returns its OS path. On failure nothing is left behind. A size <= blob.SizeUnknown
// reads raw to its end.
func (c *FileSystemCTF) stageFile(dir, tempPattern string, raw io.Reader, size int64) (string, error) {
	if c.mkdirFS != nil {
		if err := c.mkdirFS.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("unable to create directory: %w", err)
		}
	}

	osFS, ok := c.fs.(*filesystem.RootFileSystem)
	if !ok {
		return "", fmt.Errorf("atomic writes not supported on non-OS filesystem: %T", c.fs)
	}

	tempFile, err := os.CreateTemp(filepath.Join(osFS.String(), dir), tempPattern)
	if err != nil {
		return "", fmt.Errorf("unable to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	if size <= blob.SizeUnknown {
		buf := ioBufPool.Get().(*[]byte)
		defer ioBufPool.Put(buf)
		_, err = io.CopyBuffer(tempFile, raw, *buf)
	} else {
		_, err = io.CopyN(tempFile, raw, size)
	}
	if err != nil {
		return "", fmt.Errorf("unable to write content: %w", errors.Join(err, tempFile.Close(), os.Remove(tempPath)))
	}

	// Close file before chmod and rename (required for Windows compatibility)
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("unable to close temp file: %w", errors.Join(err, os.Remove(tempPath)))
	}
	return tempPath, nil
}

// publishFile makes the staged file read-only and atomically renames it to fileName in
// the directory it was staged in. On failure the caller removes the staged file.
func publishFile(staged, fileName string) error {
	if err := os.Chmod(staged, 0o444); err != nil {
		return fmt.Errorf("unable to make temp file readonly: %w", err)
	}
	if err := os.Rename(staged, filepath.Join(filepath.Dir(staged), fileName)); err != nil {
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

// SaveBlob stores b under its digest in BlobsDirectoryName without buffering it in memory.
//
// The content is staged in a temporary file first, and b is asked for its digest only
// once its content has been read in full and closed: asking earlier could make b buffer
// its content to compute the digest, and b may compute it while being read. Closing can
// fail when the content does not match the digest (e.g. a verifying reader); then
// nothing is stored.
//
// Blobs are content-addressed, so an already stored blob holds the same content and is
// kept, dropping the staged copy. Not replacing it also means a blob is never renamed
// over while a reader may hold it open: readers do not lock against writers for the
// whole read, and renaming over an open file fails on Windows. For the same reason a
// CTF that cannot be written to still accepts a blob it already stores.
func (c *FileSystemCTF) SaveBlob(ctx context.Context, b blob.ReadOnlyBlob) (err error) {
	digestable, ok := b.(blob.DigestAware)
	if !ok {
		return errors.New("blob does not have a digest that can be used to save it")
	}

	data, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("unable to read blob: %w", err)
	}
	ctxRead, err := newCtxReader(ctx, data)
	if err != nil {
		return errors.Join(fmt.Errorf("unable to create context reader: %w", err), data.Close())
	}

	// staged is cleared once published; anything still staged on return is removed.
	var staged string
	defer func() {
		if staged != "" {
			err = errors.Join(err, os.Remove(staged))
		}
	}()

	osFS, writable := c.fs.(*filesystem.RootFileSystem)
	writable = writable && !osFS.ReadOnly()
	if writable {
		// The size is passed as unknown so the content is read to its end, whatever size b
		// may report: b can only vouch for its digest once it has seen all of it.
		staged, err = c.stageFile(BlobsDirectoryName, ".upload.*", ctxRead, blob.SizeUnknown)
	} else {
		_, err = io.Copy(io.Discard, ctxRead)
	}
	if err = errors.Join(err, data.Close()); err != nil {
		return err
	}

	dig, known := digestable.Digest()
	if !known {
		return errors.New("blob does not have a digest that can be used to save it")
	}
	file, err := ToBlobFileName(dig)
	if err != nil {
		return err
	}
	if c.statFS != nil {
		if _, statErr := c.statFS.Stat(filepath.Join(BlobsDirectoryName, file)); statErr == nil {
			return nil
		}
	}
	if !writable {
		return fmt.Errorf("unable to save blob %s to %T: %w", dig, c.fs, filesystem.ErrReadOnly)
	}
	if err := publishFile(staged, file); err != nil {
		return err
	}
	staged = ""
	return nil
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
