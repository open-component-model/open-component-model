package download

import "golang.org/x/crypto/ssh"

const DefaultMaxArchiveSize int64 = 1 << 30

type Options struct {
	TempDir         string
	MaxArchiveSize  int64
	CABundle        []byte
	HostKeyCallback ssh.HostKeyCallback
}
