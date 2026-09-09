package download

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

func authMethod(ep *transport.Endpoint, creds *credsv1.GitCredentials, opts Options) (transport.AuthMethod, error) {
	if creds == nil {
		return nil, nil
	}

	switch {
	case creds.PrivateKey != "":
		if ep.Protocol != "ssh" {
			return nil, fmt.Errorf("SSH private keys require an SSH repository")
		}

		username := creds.Username
		if username == "" {
			username = ep.User
		}

		if username == "" {
			username = "git"
		}

		auth, err := gitssh.NewPublicKeysFromFile(username, creds.PrivateKey, creds.Password)
		if err != nil {
			return nil, fmt.Errorf("cannot load SSH private key: %w", err)
		}

		auth.HostKeyCallback = opts.HostKeyCallback
		return auth, nil
	case creds.Token != "":
		if ep.Protocol != "https" && ep.Protocol != "http" {
			return nil, fmt.Errorf("tokens require an HTTP or HTTPS repository")
		}

		return &githttp.TokenAuth{Token: creds.Token}, nil
	case creds.Username != "":
		if ep.Protocol != "https" && ep.Protocol != "http" {
			return nil, fmt.Errorf("username/password authentication requires an HTTP or HTTPS repository")
		}

		return &githttp.BasicAuth{Username: creds.Username, Password: creds.Password}, nil
	case creds.Password != "":
		return nil, fmt.Errorf("password requires a username or SSH private key")
	default:
		return nil, nil
	}
}
