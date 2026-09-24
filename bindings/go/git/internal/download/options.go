package download

import "golang.org/x/crypto/ssh"

const DefaultMaxArchiveSize int64 = 1 << 30

type Options struct {
	TempDir string
	// MaxArchiveSize caps the final compressed archive bytes, not the Git transfer
	// or uncompressed tree. Non-positive values disable the limit.
	MaxArchiveSize int64

	HostKeyCallback ssh.HostKeyCallback
}
