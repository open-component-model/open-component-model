//go:build !windows

package download

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing/transport/ssh/sshagent"
	"golang.org/x/crypto/ssh/agent"
)

func openSSHAgent() (agent.Agent, io.Closer, error) {
	return sshagent.New()
}
