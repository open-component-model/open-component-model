package download

import (
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/client"
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// authMethod returns a client.SSHAuth or client.HTTPAuth, or nil to leave the
// authentication to go-git.
func authMethod(ep *endpoint.Endpoint, credentials runtime.Typed, opts Options) (any, error) {
	switch creds := credentials.(type) {
	case nil:
		return legacyAuthMethod(ep, nil, opts)
	case *credsv1.GitCredentials:
		return legacyAuthMethod(ep, creds, opts)
	case *credsv1.GitBasicCredentials:
		return legacyAuthMethod(ep, &credsv1.GitCredentials{Username: creds.Username, Password: creds.Password}, opts)
	case *credsv1.GitBearerCredentials:
		return legacyAuthMethod(ep, &credsv1.GitCredentials{Token: creds.Token}, opts)
	case *credsv1.GitSSHCredentials:
		if ep.Protocol != "ssh" {
			return nil, fmt.Errorf("SSH credentials require an SSH repository")
		}
		if creds.PrivateKey != "" || creds.PrivateKeyPEM != "" {
			return legacyAuthMethod(ep, &credsv1.GitCredentials{Username: creds.Username, PrivateKey: creds.PrivateKey, PrivateKeyPEM: creds.PrivateKeyPEM, Password: creds.Passphrase}, opts)
		}
		username := creds.Username
		if username == "" {
			username = ep.User
		}
		if username == "" {
			username = "git"
		}
		auth, err := gitssh.NewSSHAgentAuth(username)
		if err != nil {
			return nil, fmt.Errorf("cannot use SSH agent: %w", err)
		}
		auth.HostKeyCallback = opts.HostKeyCallback
		return auth, nil
	default:
		return nil, fmt.Errorf("unsupported git credential type %T", credentials)
	}
}

func legacyAuthMethod(ep *endpoint.Endpoint, creds *credsv1.GitCredentials, opts Options) (any, error) {
	if creds == nil {
		creds = &credsv1.GitCredentials{}
	}

	// go-git turns URL userinfo into basic auth, which plain HTTP would send in clear text.
	if ep.Protocol == "http" && (ep.User != "" || ep.Password != "") {
		return nil, fmt.Errorf("the repository URL contains credentials; use an HTTPS repository so they are not sent in clear text")
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

// authOption configures the client with an authentication from authMethod.
func authOption(auth any) (client.Option, bool) {
	switch auth := auth.(type) {
	case client.SSHAuth:
		return client.WithSSHAuth(auth), true
	case client.HTTPAuth:
		return client.WithHTTPAuth(auth), true
	default:
		return nil, false
	}
}
