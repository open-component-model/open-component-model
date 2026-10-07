package download

import (
	"net/http"

	"golang.org/x/crypto/ssh"
)

// DefaultMaxArchiveSize is the default maximum archive size. Zero means unlimited:
// the archive is streamed to disk, so it is bounded by free disk rather than by RAM.
const DefaultMaxArchiveSize int64 = 0

type Options struct {
	TempDir string
	// MaxArchiveSize caps the final compressed archive bytes, not the Git transfer
	// or uncompressed tree. Non-positive values disable the limit.
	MaxArchiveSize int64

	HostKeyCallback ssh.HostKeyCallback
	// HTTPClient serves http(s) repositories. Nil uses the shared OCM client defaults.
	HTTPClient *http.Client
}
