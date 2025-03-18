package ctf

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

// FileFormat represents the format of a CTF.
// A FileFormat can be translated to any other FileFormat without loss of information.
// The zero value of FileFormat is FormatDirectory, and also the default.
type FileFormat int

var ErrUnsupportedFormat = fmt.Errorf("unsupported format")

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
var formats = [3]string{"directory", "tar", "tgz"}

func (f FileFormat) String() string {
	return formats[f]
}

// Flags to OpenCTF. They are not bound to a type because the underlying type changes based on syscall interfaces.
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
	// GetIndex returns the artifact index of the CTF.
	GetIndex() (v1.Index, error)
	// SetIndex sets the artifact index of the CTF.
	SetIndex(index v1.Index) (err error)
}

// BlobStore provides access to the blobs of a CTF.
type BlobStore interface {
	// ListBlobs returns a list of all blobs in the CTF irrespective of if they are referenced by the index.
	ListBlobs() ([]string, error)
	// GetBlob returns the blob with the specified digest.
	GetBlob(digest string) (blob.ReadOnlyBlob, error)
	// SaveBlob saves the blob to the CTF.
	SaveBlob(blob blob.ReadOnlyBlob) (err error)
	// DeleteBlob deletes the blob with the specified digest from the CTF.
	DeleteBlob(digest string) (err error)
}

// OpenCTF opens a CTF at the specified path with the specified format.
// the CTF may be backed by a temporary directory if the format is FormatTAR or FormatTGZ.
// In this case, the temporary directory is used to extract the archive before returning access on that path.
func OpenCTF(path string, format FileFormat, flag int) (CTF, error) {
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

		return ExtractTAR(tmp, path, format, flag)
	default:
		return nil, ErrUnsupportedFormat
	}
}

// OpenCTFByFileExtension opens a CTF at the specified path by determining the format from the file extension.
// For FormatDirectory, the path is treated as a directory, otherwise the path is interpreted as a file with
// an extension that determines its behavior.
// For more information on how flag behaves for FormatTAR (and FormatTGZ), see ExtractTAR.
func OpenCTFByFileExtension(path string, flag int) (archive CTF, discovered FileFormat, err error) {
	ext := filepath.Ext(path)
	switch ext {
	case ".tgz", ".tar.gz":
		discovered = FormatTGZ
	case ".tar":
		discovered = FormatTAR
	default:
		discovered = FormatDirectory
	}

	if archive, err = OpenCTF(path, discovered, flag); err != nil {
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
func WorkWithinCTF(path string, flag int, work func(ctf CTF) error) error {
	archive, format, err := OpenCTFByFileExtension(path, flag)
	if err != nil {
		return fmt.Errorf("failed to open CTF %q: %w", path, err)
	}

	if err := work(archive); err != nil {
		return fmt.Errorf("failed to work within CTF at %q: %w", path, err)
	}

	if flag&O_RDWR != 0 && (format == FormatTAR || format == FormatTGZ) {
		slog.Debug(
			"work within ctf has concluded and format and mode indicates it needs to be rearchived, this might take a while",
			slog.String("path", path),
			slog.String("format", format.String()),
			slog.String("mode", "readwrite"),
		)
		if err := Archive(archive, path, format); err != nil {
			return fmt.Errorf("failed to archive CTF %q: %w", path, err)
		}
	}

	return nil
}
