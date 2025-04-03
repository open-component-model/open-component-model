package ctf

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

// FileFormat represents the format of a CTF.
// A FileFormat can be translated to any other FileFormat without loss of information.
// The zero value of FileFormat is FormatDirectory, and also the default.
type FileFormat int

var ErrUnsupportedFormat = errors.New("unsupported format")

const (
	// FormatUnknown represents an unknown format.
	FormatUnknown FileFormat = iota
	// FormatDirectory represents a CTF stored as a directory at a root path.
	FormatDirectory FileFormat = iota
	// FormatTAR represents a CTF stored as a Tape (TAR) archive.
	FormatTAR FileFormat = iota
	// FormatTGZ represents a CTF stored as a Tape (TAR) archive compressed with GZip with arbitrary compression.
	FormatTGZ FileFormat = iota
)

// formats is a list of all supported formats corresponding to the FileFormat constants.
var formats = [4]string{"unknown", "directory", "tar", "tgz"}

func (f FileFormat) String() string {
	return formats[f]
}

// Flags to OpenCTF. They are not bound to a type because the underlying type changes based on syscall interfaces.
//
//nolint:stylecheck // ignore style error, as the flags replicating the os package mode
const (
	// O_RDONLY indicates that the CTF is opened in read-only mode.
	O_RDONLY = os.O_RDONLY
	// O_RDWR indicates that the CTF is opened in read-write mode.
	O_RDWR = os.O_RDWR
	// O_CREATE indicates that the CTF is created if it does not exist.
	O_CREATE = os.O_CREATE
)

// CTF represents the CommonTransportFormat. It is an interface that provides access to an index and blobs through
// IndexStore and BlobStore interfaces.
// Depending on the FileFormat, the CTF may be backed by a filesystem or an archive.
//
// In practice, the CTF is almost always backed with FormatDirectory, and working on FormatTAR and FormatTGZ is
// handled by
// 1. Extracting the CTF into a Directory format
// 2. Working on the Directory format
// 3. Archiving the Directory format back into the original format
type CTF interface {
	Format() FileFormat

	IndexStore
	BlobStore
}

// IndexStore provides access to the index of a CTF.
type IndexStore interface {
	ReadOnlyIndexStore
	// SetIndex sets the artifact index of the CTF.
	SetIndex(ctx context.Context, index v1.Index) (err error)
}

type ReadOnlyIndexStore interface {
	// GetIndex returns the artifact index of the CTF.
	GetIndex(ctx context.Context) (v1.Index, error)
}

// BlobStore provides access to the blobs of a CTF.
type BlobStore interface {
	ReadOnlyBlobStore
	// SaveBlob saves the blob to the CTF.
	SaveBlob(ctx context.Context, blob blob.ReadOnlyBlob) (err error)
	// DeleteBlob deletes the blob with the specified digest from the CTF.
	DeleteBlob(ctx context.Context, digest string) (err error)
}

type ReadOnlyBlobStore interface {
	// ListBlobs returns a list of all blobs in the CTF irrespective of if they are referenced by the index.
	ListBlobs(ctx context.Context) ([]string, error)
	// GetBlob returns the blob with the specified digest.
	GetBlob(ctx context.Context, digest string) (blob.ReadOnlyBlob, error)
}

// OpenCTF opens a CTF at the specified path with the specified format.
// the CTF may be backed by a temporary directory if the format is FormatTAR or FormatTGZ.
// In this case, the temporary directory is used to extract the archive before returning access on that path.
func OpenCTF(ctx context.Context, path string, format FileFormat, flag int) (CTF, error) {
	switch format {
	case FormatDirectory:
		ctf, err := OpenCTFFromOSPath(path, flag)
		if err != nil {
			return nil, fmt.Errorf("unable to open filesystem ctf: %w", err)
		}
		return ctf, nil
	case FormatTAR, FormatTGZ:
		hash := fnv.New32a()
		if _, err := hash.Write([]byte(path)); err != nil {
			return nil, fmt.Errorf("unable to hash path to determine temporary ctf: %w", err)
		}

		tmp, err := os.MkdirTemp("", fmt.Sprintf("ctf-%x-*", hash.Sum(nil)))
		if err != nil {
			return nil, fmt.Errorf("unable to create temporary directory to extract ctf: %w", err)
		}
		slog.Debug("ctf is automatically extracted and will need to be rearchived to persist", slog.String("path", tmp))

		ctf, err := ExtractTAR(ctx, tmp, path, format, flag)
		if errors.Is(err, os.ErrNotExist) && flag&O_CREATE != 0 {
			return OpenCTFFromOSPath(tmp, flag)
		}
		return ctf, nil
	default:
		return nil, ErrUnsupportedFormat
	}
}

// OpenCTFFromOSPath opens a CTF at the specified path with the specified flags.
// Supported flags are O_RDONLY, O_RDWR, and O_CREATE, other flags can lead to undefined behavior.
func OpenCTFFromOSPath(path string, flag int) (*FileSystemCTF, error) {
	fileSystem, err := filesystem.NewFS(path, flag)
	if err != nil {
		return nil, fmt.Errorf("unable to setup file system: %w", err)
	}
	return NewFileSystemCTF(fileSystem), nil
}

// OpenCTFByFileExtension opens a CTF at the specified path by determining the format from the file extension.
// For FormatDirectory, the path is treated as a directory, otherwise the path is interpreted as a file with
// an extension that determines its behavior.
// For more information on how flag behaves for FormatTAR (and FormatTGZ), see ExtractTAR.
func OpenCTFByFileExtension(ctx context.Context, path string, flag int) (archive CTF, discovered FileFormat, err error) {
	ext := filepath.Ext(path)
	// check if the extension is in form of ".tar.gz" in which case the extension is ".tar" and ".gz"
	// but filepath.Ext only returns ".gz". Then we need to check if previous extension is ".tar"
	if doubleGzExt := ".gz"; ext == doubleGzExt {
		ext = filepath.Ext(path[:len(path)-len(doubleGzExt)]) + ext
	}
	switch ext {
	case ".tgz", ".tar.gz":
		discovered = FormatTGZ
	case ".tar":
		discovered = FormatTAR
	default:
		discovered = FormatDirectory
	}

	if archive, err = OpenCTF(ctx, path, discovered, flag); err != nil {
		return nil, FormatUnknown, fmt.Errorf("failed to open CTF: %w", err)
	}

	return archive, discovered, nil
}

// WorkWithinCTF opens a CTF at the specified path and calls the work function with the CTF.
// If the CTF is backed by a TAR or TGZ archive, the CTF is archived into its originally discovered
// format after the work function is called.
// If an error occurs during the work function, the CTF is not archived if the format is FormatTAR or FormatTGZ
// However, if the format is FormatDirectory, the CTF is edited in place, which can lead to non atomic failures.
// To avoid this, by default (flag not set to O_RDWR), the CTF is not rearchived and opened in read-only mode.
func WorkWithinCTF(ctx context.Context, path string, flag int, work func(ctx context.Context, ctf CTF) error) error {
	archive, format, err := OpenCTFByFileExtension(ctx, path, flag)
	if err != nil {
		return fmt.Errorf("failed to open CTF %q: %w", path, err)
	}

	if err := work(ctx, archive); err != nil {
		return fmt.Errorf("failed to work within CTF at %q: %w", path, err)
	}

	if flag&O_RDWR != 0 && (format == FormatTAR || format == FormatTGZ) {
		slog.Debug(
			"work within ctf has concluded and format and mode indicates it needs to be rearchived, this might take a while",
			slog.String("path", path),
			slog.String("format", format.String()),
			slog.String("mode", "readwrite"),
		)
		if err := Archive(ctx, archive, path, format); err != nil {
			return fmt.Errorf("failed to archive CTF %q: %w", path, err)
		}
	}

	return nil
}
