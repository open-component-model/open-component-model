package ctf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

const (
	BlobsDirectoryName = "blobs"
)

type FileSystemCTF struct {
	filesystem.FileSystem
}

var (
	_ CTF                   = (*FileSystemCTF)(nil)
	_ filesystem.FileSystem = (*FileSystemCTF)(nil)
)

func OpenCTFFromOSPath(path string, flag int) (*FileSystemCTF, error) {
	fileSystem, err := filesystem.NewFS(path, flag)
	if err != nil {
		return nil, fmt.Errorf("unable to setup file system: %w", err)
	}
	return OpenCTFFromFilesystem(fileSystem), nil
}

func OpenCTFFromFilesystem(fileSystem filesystem.FileSystem) *FileSystemCTF {
	return &FileSystemCTF{
		FileSystem: fileSystem,
	}
}

func (c *FileSystemCTF) FS() filesystem.FileSystem {
	return c.FileSystem
}

func (c *FileSystemCTF) Format() FileFormat {
	return FormatDirectory
}

func (c *FileSystemCTF) GetIndex() (index v1.Index, err error) {
	fi, err := c.FileSystem.Stat(v1.ArtifactIndexFileName)
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
	if indexFile, err = c.FileSystem.Open(v1.ArtifactIndexFileName); err != nil {
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

func (c *FileSystemCTF) SetIndex(index v1.Index) (err error) {
	data, err := v1.Encode(index)
	if err != nil {
		return fmt.Errorf("unable to encode artifact index: %w", err)
	}

	return c.WriteFile(v1.ArtifactIndexFileName, bytes.NewReader(data))
}

func (c *FileSystemCTF) WriteFile(name string, raw io.Reader) (err error) {
	if err := c.FileSystem.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return fmt.Errorf("unable to create directory: %w", err)
	}
	var file fs.File
	if file, err = c.FileSystem.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0644); err != nil {
		return fmt.Errorf("unable to open artifact index: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	writeable, ok := file.(io.Writer)
	if !ok {
		return fmt.Errorf("file %s is read only and cannot be saved", name)
	}

	if _, err = io.Copy(writeable, raw); err != nil {
		return fmt.Errorf("unable to write artifact index: %w", err)
	}

	return nil
}

func (c *FileSystemCTF) DeleteBlob(digest string) (err error) {
	if err = c.FileSystem.Remove(filepath.Join(BlobsDirectoryName, ToBlobFileName(digest))); err != nil {
		return fmt.Errorf("unable to delete blob: %w", err)
	}

	return nil
}

func (c *FileSystemCTF) GetBlob(digest string) (blob.ReadOnlyBlob, error) {
	b := NewCASFileBlob(c.FileSystem, filepath.Join(BlobsDirectoryName, ToBlobFileName(digest)))
	b.SetPrecalculatedDigest(digest)
	return b, nil
}

func (c *FileSystemCTF) ListBlobs() (digests []string, err error) {
	dir, err := c.FileSystem.ReadDir(BlobsDirectoryName)
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

func (c *FileSystemCTF) SaveBlob(b blob.ReadOnlyBlob) (err error) {
	digestable, ok := b.(blob.DigestAware)
	if !ok {
		return errors.New("blob does not have a digest that can be used to save it")
	}

	data, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("unable to read blob: %w", err)
	}
	defer func() {
		err = errors.Join(err, data.Close())
	}()

	dig, known := digestable.Digest()
	if !known {
		return errors.New("blob does not have a digest that can be used to save it")
	}

	return c.WriteFile(filepath.Join(
		BlobsDirectoryName,
		ToBlobFileName(dig),
	), data)
}

func ToBlobFileName(digest string) string {
	return strings.ReplaceAll(digest, ":", ".")
}

func ToDigest(blobFileName string) string {
	return strings.ReplaceAll(blobFileName, ".", ":")
}
