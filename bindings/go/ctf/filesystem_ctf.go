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
type FileSystemCTF struct {
	fs filesystem.FileSystem
}

var _ CTF = (*FileSystemCTF)(nil)

// NewFileSystemCTF opens a CTF with the specified filesystem as its root
func NewFileSystemCTF(filesystem filesystem.FileSystem) *FileSystemCTF {
	return &FileSystemCTF{
		fs: filesystem,
	}
}

// FS returns the underlying filesystem.FileSystem of the CTF.
// Note that write operations to the FileSystem can affect the integrity of the CTF.
// TODO(jakobmoellerdev): restrict returned FileSystem to only allow read operations.
func (c *FileSystemCTF) FS() filesystem.FileSystem {
	return c.fs
}

// Format always returns FormatDirectory for FileSystemCTF.
func (c *FileSystemCTF) Format() FileFormat {
	return FormatDirectory
}

// GetIndex returns the v1.ArtifactIndexFileName parsed as v1.Index of the CTF.
// If the CTF is empty, an empty index is returned so it can be set with SetIndex.
func (c *FileSystemCTF) GetIndex(_ context.Context) (index v1.Index, err error) {
	fi, err := c.fs.Stat(v1.ArtifactIndexFileName)
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
func (c *FileSystemCTF) writeFile(name string, raw io.Reader, size int64) (err error) {
	if err := c.fs.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("unable to create directory: %w", err)
	}
	var file fs.File
	if file, err = c.fs.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
		return fmt.Errorf("unable to open artifact index: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	writeable, ok := file.(io.Writer)
	if !ok {
		return fmt.Errorf("file %s is read only and cannot be saved", name)
	}

	if size <= blob.SizeUnknown {
		buf := ioBufPool.Get().(*[]byte)
		defer ioBufPool.Put(buf)
		if _, err = io.CopyBuffer(writeable, raw, *buf); err != nil {
			return fmt.Errorf("unable to write artifact index: %w", err)
		}
	} else {
		if _, err = io.CopyN(writeable, raw, size); err != nil {
			return fmt.Errorf("unable to write artifact index: %w", err)
		}
	}

	return nil
}

// DeleteBlob deletes the blob with the given digest from the CTF by removing the file from BlobsDirectoryName.
func (c *FileSystemCTF) DeleteBlob(_ context.Context, digest string) (err error) {
	file, err := ToBlobFileName(digest)
	if err != nil {
		return err
	}
	if err = c.fs.Remove(filepath.Join(BlobsDirectoryName, file)); err != nil {
		return fmt.Errorf("unable to delete blob: %w", err)
	}

	return nil
}

// GetBlob returns the blob with the given digest from the CTF by reading the file from BlobsDirectoryName.
func (c *FileSystemCTF) GetBlob(_ context.Context, digest string) (blob.ReadOnlyBlob, error) {
	file, err := ToBlobFileName(digest)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(BlobsDirectoryName, file)
	if _, err := c.fs.Stat(path); err != nil {
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
	dir, err := c.fs.ReadDir(BlobsDirectoryName)
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

	size := blob.SizeUnknown
	if sizeable, ok := b.(blob.SizeAware); ok {
		size = sizeable.Size()
	}

	data, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("unable to read blob: %w", err)
	}
	defer func() {
		// first close the data stream, then delete the blob if closing fails
		// this is important to avoid having a dangling possibly corrupt blob in the CTF
		if err = errors.Join(err, data.Close()); err != nil {
			// if closing the data stream fails, we need to delete the blob
			// but we should use a background context as the original ctx might already be cancelled
			err = errors.Join(err, c.DeleteBlob(context.Background(), dig))
		}
	}()

	file, err := ToBlobFileName(dig)
	if err != nil {
		return err
	}

	ctxRead, err := newCtxReader(ctx, data)
	if err != nil {
		return fmt.Errorf("unable to create context reader: %w", err)
	}

	return c.writeFile(filepath.Join(
		BlobsDirectoryName,
		file,
	), ctxRead, size)
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
