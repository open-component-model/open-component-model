package download

import (
	"io"
	"os"

	"github.com/go-git/go-git/v6/plumbing/transport/ssh/sshagent"
	"golang.org/x/crypto/ssh/agent"
)

func openSSHAgent() (agent.Agent, io.Closer, error) {
	// go-git's Windows helper discards the native pipe handle, so open it ourselves.
	connection, err := os.OpenFile(`\\.\pipe\openssh-ssh-agent`, os.O_RDWR, 0)
	if err == nil {
		return agent.NewClient(connection), connection, nil
	}
	// Pageant uses window messages rather than a persistent socket connection.
	return sshagent.New()
}
