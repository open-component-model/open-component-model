package ctf

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

// ExtractTAR extracts a CTF from a file at the given path and writes it to the given base directory.
// The base directory must exist and will form the parent directory of the extracted CTF.
// The format of the file must be one of the supported formats (FormatTAR, FormatTGZ).
// The extracted CTF is not modified and only read from after extraction,
// and the TAR itself is not modified.
// If the flag O_RDONLY is set, the extracted CTF will be read-only as well, however
// the CTF will be first opened as O_RDWR to copy the data from the TAR into the new FileSystemCTF.
func ExtractTAR(base, path string, format FileFormat, flag int) (extracted *FileSystemCTF, err error) {
	if format == FormatDirectory {
		return nil, ErrUnsupportedFormat
	}

	var tarFile *os.File
	if tarFile, err = os.Open(path); err != nil {
		return nil, fmt.Errorf("unable to open tar file: %w", err)
	}
	defer func() {
		err = errors.Join(err, tarFile.Close())
	}()

	// for the extracted version we will first open the CTF with O_RDWR
	ctf, err := OpenCTFFromOSPath(base, O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("unable to setup file system ctf: %w", err)
	}

	var reader *tar.Reader
	if format == FormatTGZ {
		gzipped, err := gzip.NewReader(tarFile)
		if err != nil {
			return nil, fmt.Errorf("unable to create gzip reader: %w", err)
		}
		defer func() {
			err = errors.Join(err, gzipped.Close())
		}()
		reader = tar.NewReader(gzipped)
	} else {
		reader = tar.NewReader(tarFile)
	}

	if err := extractTARToFilesystemCTF(reader, ctf); err != nil {
		return nil, fmt.Errorf("unable to extract tar to filesystem ctf: %w", err)
	}

	// if we have an original flag, we will now respect the flag and set the FS to read-only if O_RDONLY is set
	// this makes sure that even though we just extracted the tar, it can only be read from.
	if flag&O_RDONLY != 0 || (flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0) {
		ctf.FS().ForceReadOnly()
	}

	return ctf, nil
}

func extractTARToFilesystemCTF(reader *tar.Reader, ctf *FileSystemCTF) (err error) {
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if strings.Contains(header.Name, "..") {
			return fmt.Errorf("invalid tar entry, contains %q: %s", "..", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeReg:
			if err := ctf.WriteFile(header.Name, reader); err != nil {
				return fmt.Errorf("unable to write file: %w", err)
			}
		case tar.TypeDir:
			if err = os.MkdirAll(header.Name, 0o755); err != nil {
				return fmt.Errorf("unable to create directory: %w", err)
			}
		}
	}

	return nil
}

// Archive creates an archive from the provided CTF and writes it to the specified path.
// The format of the archive is determined by the format parameter.
// Supported formats are FormatTAR, FormatTGZ, and FormatDirectory.
// If the format is FormatDirectory, the filesystem is copied to the specified path.
func Archive(ctf CTF, path string, format FileFormat) error {
	switch format {
	case FormatDirectory:
		return ArchiveDirectory(ctf, path)
	case FormatTAR, FormatTGZ:
		return ArchiveTAR(ctf, path, format)
	default:
		return ErrUnsupportedFormat
	}
}

// ArchiveDirectory archives the CTF to the specified path with FormatDirectory.
// The blobs are copied to the directory and the index is written to the index file.
// The CTF is not modified and only read from.
// The directory is created if it does not exist.
// The blobs are written to the blobs directory concurrently.
func ArchiveDirectory(ctf CTF, path string) error {
	var fsCTF CTF

	blobs, err := ctf.ListBlobs()
	if err != nil {
		return fmt.Errorf("unable to list blobs: %w", err)
	}

	if fsCTF, err = OpenCTFFromOSPath(path, O_RDWR|O_CREATE); err != nil {
		return fmt.Errorf("unable to setup file system ctf: %w", err)
	}
	if err := os.Mkdir(path, 0o755); os.IsExist(err) {
	} else if err != nil {
		return fmt.Errorf("unable to create directory: %w", err)
	} else {
		if fsCTF, err = OpenCTFFromOSPath(path, O_RDWR); err != nil {
			return fmt.Errorf("unable to setup file system ctf: %w", err)
		}
	}

	if len(blobs) > 0 {
		var wg sync.WaitGroup
		wg.Add(len(blobs))
		errs := make(chan error, len(blobs))

		for _, digest := range blobs {
			go func(digest string) {
				defer wg.Done()
				b, err := ctf.GetBlob(digest)
				if err != nil {
					errs <- fmt.Errorf("unable to get blob %s: %w", digest, err)
					return
				}
				if err := fsCTF.SaveBlob(b); err != nil {
					errs <- fmt.Errorf("unable to save blob %s: %w", digest, err)
					return
				}
				errs <- nil
			}(digest)
		}
		wg.Wait()

		var err error
		for i := 0; i < len(blobs); i++ {
			err = errors.Join(<-errs)
		}
		if err != nil {
			return err
		}
	}

	idx, err := ctf.GetIndex()
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}
	if err := fsCTF.SetIndex(idx); err != nil {
		return fmt.Errorf("unable to set index: %w", err)
	}

	return nil
}

// ArchiveTAR archives the CTF to the specified path.
// The blobs are written to the blobs directory and the index is written to the index file.
// The CTF is not modified and only read from.
// The file is created if it does not exist.
//
// see ArchiveTARToWriter for more details.
func ArchiveTAR(ctf CTF, path string, format FileFormat) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("unable to open file for writing ctf archive: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	return ArchiveTARToWriter(ctf, file, format)
}

// ArchiveTARToWriter archives the CTF to the specified writer.
// The file can be optionally targeted as a tgz by specifying FormatTGZ, FormatTAR otherwise.
//
// The blobs are written to the blobs directory sequentially due to the nature of TAR archives.
// The blobs are written in the order they are returned by ListBlobs.
// The index is written to the index file as first entry.
func ArchiveTARToWriter(ctf CTF, writer io.Writer, format FileFormat) (err error) {
	if format == FormatDirectory {
		return ErrUnsupportedFormat
	}

	var tarWriter *tar.Writer
	if format == FormatTGZ {
		gzipFile := gzip.NewWriter(writer)
		defer func() {
			err = errors.Join(err, gzipFile.Close())
		}()
		tarWriter = tar.NewWriter(gzipFile)
	} else {
		tarWriter = tar.NewWriter(writer)
	}
	defer func() {
		err = errors.Join(err, tarWriter.Close())
	}()

	blobs, err := ctf.ListBlobs()
	if err != nil {
		return fmt.Errorf("unable to list blobs: %w", err)
	}

	copyBuffer := make([]byte, blob.DefaultArchiveBlobBufferSize) // shared buffer for all data to avoid allocs.

	if err := archiveIndex(ctf, tarWriter, copyBuffer); err != nil {
		return fmt.Errorf("unable to archive index: %w", err)
	}
	for _, digest := range blobs {
		b, err := ctf.GetBlob(digest)
		if err != nil {
			return fmt.Errorf("unable to get blob %s: %w", digest, err)
		}
		size, sizeAware := b.(blob.SizeAware)
		if !sizeAware {
			return fmt.Errorf("blob %s has no known size", digest)
		}
		name := filepath.Join(BlobsDirectoryName, ToBlobFileName(digest))
		if err := blob.ArchiveBlob(name, size.Size(), digest, b, tarWriter, copyBuffer); err != nil {
			return err
		}
	}

	return nil
}

func archiveIndex(ctf CTF, tarWriter *tar.Writer, buf []byte) (err error) {
	idx, err := ctf.GetIndex()
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}
	rawIdx, err := v1.Encode(idx)
	if err != nil {
		return fmt.Errorf("unable to encode index: %w", err)
	}
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: v1.ArtifactIndexFileName,
		Mode: 0o644,
		Size: int64(len(rawIdx)),
	}); err != nil {
		return fmt.Errorf("unable to write index header: %w", err)
	}
	if _, err := io.CopyBuffer(tarWriter, bytes.NewReader(rawIdx), buf); err != nil {
		return fmt.Errorf("unable to write index: %w", err)
	}
	return nil
}
