package download

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/client"
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// authMethod consumes credentials normalized by ConvertCredentials and returns
// a client.SSHAuth or client.HTTPAuth, or nil to leave authentication to go-git.
func authMethod(ep *endpoint.Endpoint, credentials runtime.Typed, opts Options) (any, error) {
	// go-git turns URL userinfo into basic auth, which plain HTTP would send in clear text.
	if ep.Protocol == "http" && (ep.User != "" || ep.Password != "") {
		return nil, fmt.Errorf("the repository URL contains credentials; use an HTTPS repository so they are not sent in clear text")
	}
	switch creds := credentials.(type) {
	case nil:
		if ep.Protocol == "ssh" {
			// Own the agent connection even when no explicit credentials were supplied.
			return sshAuthMethod(ep, &credsv1.GitSSHCredentials{}, opts)
		}
		return nil, nil
	case *credsv1.GitHTTPSCredentials:
		return httpsAuthMethod(ep, creds)
	case *credsv1.GitBearerCredentials:
		return bearerAuthMethod(ep, creds)
	case *credsv1.GitSSHCredentials:
		return sshAuthMethod(ep, creds, opts)
	default:
		return nil, fmt.Errorf("unsupported git credential type %T", credentials)
	}
}

func httpsAuthMethod(ep *endpoint.Endpoint, creds *credsv1.GitHTTPSCredentials) (client.HTTPAuth, error) {
	if ep.Protocol != "https" {
		return nil, fmt.Errorf("username/password authentication requires an HTTPS repository")
	}
	return &githttp.BasicAuth{Username: creds.Username, Password: creds.Password}, nil
}

func bearerAuthMethod(ep *endpoint.Endpoint, creds *credsv1.GitBearerCredentials) (client.HTTPAuth, error) {
	if ep.Protocol != "https" {
		return nil, fmt.Errorf("tokens require an HTTPS repository")
	}
	return &githttp.TokenAuth{Token: creds.Token}, nil
}

func sshAuthMethod(ep *endpoint.Endpoint, creds *credsv1.GitSSHCredentials, opts Options) (client.SSHAuth, error) {
	if ep.Protocol != "ssh" {
		return nil, fmt.Errorf("SSH credentials require an SSH repository")
	}
	username := creds.Username
	if username == "" {
		username = ep.User
	}
	if username == "" {
		username = "git"
	}

	if creds.PrivateKeyPEM == "" && creds.PrivateKey == "" {
		sshAgent, connection, err := openSSHAgent()
		if err != nil {
			return nil, fmt.Errorf("cannot use SSH agent: %w", err)
		}
		auth := &gitssh.PublicKeysCallback{User: username, Callback: sshAgent.Signers}
		auth.HostKeyCallback = opts.HostKeyCallback
		return &sshAgentAuth{PublicKeysCallback: auth, connection: connection}, nil
	}
	var auth *gitssh.PublicKeys
	var err error
	if creds.PrivateKeyPEM != "" {
		auth, err = gitssh.NewPublicKeys(username, []byte(creds.PrivateKeyPEM), creds.Passphrase)
	} else {
		auth, err = gitssh.NewPublicKeysFromFile(username, creds.PrivateKey, creds.Passphrase)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot load SSH private key: %w", err)
	}
	auth.HostKeyCallback = opts.HostKeyCallback
	return auth, nil
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

// sshAgentAuth keeps the signer connection alive until Download finishes using it.
type sshAgentAuth struct {
	*gitssh.PublicKeysCallback
	connection io.Closer
}

func (a *sshAgentAuth) Close() error {
	if a.connection == nil {
		return nil
	}
	return a.connection.Close()
}
