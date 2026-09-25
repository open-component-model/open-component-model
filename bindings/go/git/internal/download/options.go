package download

import (
	"net/http"

	"golang.org/x/crypto/ssh"
)

const DefaultMaxArchiveSize int64 = 1 << 30

type Options struct {
	TempDir string
	// MaxArchiveSize caps the final compressed archive bytes, not the Git transfer
	// or uncompressed tree. Non-positive values disable the limit.
	MaxArchiveSize int64

	HostKeyCallback ssh.HostKeyCallback
	// HTTPClient serves http(s) repositories. Nil uses the shared OCM client defaults.
	HTTPClient *http.Client
}
