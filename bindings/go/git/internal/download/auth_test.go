package download

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

func TestAuthModes(t *testing.T) {
	r := require.New(t)

	httpEndpoint, err := endpoint.Parse("https://example.com/repo")
	r.NoError(err)

	sshEndpoint, err := endpoint.Parse("git@example.com:repo")
	r.NoError(err)

	auth, err := authMethod(httpEndpoint, nil, Options{})
	r.NoError(err)
	r.Nil(auth)
	auth, err = authMethod(httpEndpoint, &credsv1.GitCredentials{Token: "token", Username: "ignored", Password: "ignored"}, Options{})
	r.NoError(err)
	r.IsType(&githttp.TokenAuth{}, auth)
	auth, err = authMethod(httpEndpoint, &credsv1.GitCredentials{Username: "user", Password: "password"}, Options{})
	r.NoError(err)
	r.IsType(&githttp.BasicAuth{}, auth)
	_, err = authMethod(httpEndpoint, &credsv1.GitCredentials{Password: "missing-user"}, Options{})
	r.Error(err)

	_, err = authMethod(httpEndpoint, &credsv1.GitCredentials{PrivateKey: "/keys/key"}, Options{})
	r.Error(err)

	_, err = authMethod(sshEndpoint, &credsv1.GitCredentials{Token: "token"}, Options{})
	r.Error(err)

	_, err = authMethod(sshEndpoint, &credsv1.GitCredentials{Username: "user", Password: "password"}, Options{})
	r.Error(err)

	_, private, err := ed25519.GenerateKey(rand.Reader)
	r.NoError(err)
	block, err := ssh.MarshalPrivateKey(private, "")
	r.NoError(err)
	auth, err = authMethod(sshEndpoint, &credsv1.GitCredentials{PrivateKeyPEM: string(pem.EncodeToMemory(block)), PrivateKey: "/does/not/exist"}, Options{HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	r.NoError(err, "inline key takes precedence over the file")
	r.IsType(&gitssh.PublicKeys{}, auth)
	r.NotNil(auth.(*gitssh.PublicKeys).HostKeyCallback)

	auth, err = authMethod(sshEndpoint, nil, Options{})
	r.NoError(err)
	r.Nil(auth, "without a host key callback go-git builds the SSH agent auth itself")

	t.Setenv("SSH_AUTH_SOCK", "")
	_, err = authMethod(sshEndpoint, nil, Options{HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	r.ErrorContains(err, "SSH agent")
}
