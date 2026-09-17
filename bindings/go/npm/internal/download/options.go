package download

import "net/http"

const (
	// DefaultMaxDownloadSize is the default maximum tarball size. Zero means unlimited:
	// tarballs are streamed to disk rather than held in memory, so a download is bounded
	// by free disk rather than by RAM.
	DefaultMaxDownloadSize int64 = 0

	// DefaultMaxMetadataSize caps the registry metadata document. The version document
	// of a package is a few kilobytes, but the packument fallback carries every
	// version of a package and decodes into a map held in memory.
	DefaultMaxMetadataSize int64 = 64 << 20
)

// Options configures a single [Download] call.
type Options struct {
	// Client is the HTTP client used for the requests. Nil uses http.DefaultClient.
	Client *http.Client

	// MaxDownloadSize limits the number of bytes read from the tarball response.
	// Zero or negative means unlimited.
	MaxDownloadSize int64

	// MaxMetadataSize limits the number of bytes read from a metadata response.
	// Zero applies DefaultMaxMetadataSize, negative means unlimited.
	MaxMetadataSize int64

	// TempDir is the directory the tarball is written to. Empty uses the OS
	// temporary directory. The file backing the returned blob is created here and
	// outlives [Download], so the caller owns its lifetime.
	TempDir string
}

func (o Options) client() *http.Client {
	if o.Client == nil {
		return http.DefaultClient
	}

	return o.Client
}
