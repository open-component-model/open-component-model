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
		creds = &credsv1.GitCredentials{}
	}

	// go-git turns URL userinfo into basic auth, which plain HTTP would send in clear text.
	if ep.Protocol == "http" && (ep.User != "" || ep.Password != "") {
		return nil, fmt.Errorf("credentials require an HTTPS repository")
	}

	switch {
	case creds.PrivateKeyPEM != "" || creds.PrivateKey != "":
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

		var auth *gitssh.PublicKeys
		var err error
		if creds.PrivateKeyPEM != "" {
			auth, err = gitssh.NewPublicKeys(username, []byte(creds.PrivateKeyPEM), creds.Password)
		} else {
			auth, err = gitssh.NewPublicKeysFromFile(username, creds.PrivateKey, creds.Password)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot load SSH private key: %w", err)
		}

		auth.HostKeyCallback = opts.HostKeyCallback
		return auth, nil
	case creds.Token != "":
		if ep.Protocol != "https" {
			return nil, fmt.Errorf("tokens require an HTTPS repository")
		}

		return &githttp.TokenAuth{Token: creds.Token}, nil
	case creds.Username != "":
		if ep.Protocol != "https" {
			return nil, fmt.Errorf("username/password authentication requires an HTTPS repository")
		}

		return &githttp.BasicAuth{Username: creds.Username, Password: creds.Password}, nil
	case creds.Password != "":
		return nil, fmt.Errorf("password requires a username or SSH private key")
	case ep.Protocol == "ssh" && opts.HostKeyCallback != nil:
		// go-git falls back to the SSH agent on its own, but then ignores the host key callback.
		auth, err := gitssh.NewSSHAgentAuth(ep.User)
		if err != nil {
			return nil, fmt.Errorf("cannot use SSH agent: %w", err)
		}

		auth.HostKeyCallback = opts.HostKeyCallback
		return auth, nil
	default:
		return nil, nil
	}
}
