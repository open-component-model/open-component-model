package download

import "golang.org/x/crypto/ssh"

const DefaultMaxDownloadSize int64 = 1 << 30

type Options struct {
	TempDir         string
	MaxDownloadSize int64
	CABundle        []byte
	HostKeyCallback ssh.HostKeyCallback
}
