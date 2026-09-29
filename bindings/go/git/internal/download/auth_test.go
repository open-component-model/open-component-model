package download

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"testing"

	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

func TestAuthModes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		repository string
		creds      *credsv1.GitCredentials
		want       any
		wantErr    string
	}{
		{name: "anonymous HTTPS", repository: "https://example.com/repo"},
		{name: "anonymous HTTP", repository: "http://example.com/repo"},
		{
			name: "token over basic", repository: "https://example.com/repo",
			creds: &credsv1.GitCredentials{Token: "token", Username: "ignored", Password: "ignored"},
			want:  &githttp.TokenAuth{Token: "token"},
		},
		{
			name: "basic", repository: "https://example.com/repo",
			creds: &credsv1.GitCredentials{Username: "user", Password: "password"},
			want:  &githttp.BasicAuth{Username: "user", Password: "password"},
		},
		{
			name: "password without username", repository: "https://example.com/repo",
			creds:   &credsv1.GitCredentials{Password: "missing-user"},
			wantErr: "password requires a username or SSH private key",
		},
		{
			name: "SSH key on HTTPS", repository: "https://example.com/repo",
			creds:   &credsv1.GitCredentials{PrivateKey: "/keys/key"},
			wantErr: "SSH private keys require an SSH repository",
		},
		{
			name: "token on HTTP", repository: "http://example.com/repo",
			creds:   &credsv1.GitCredentials{Token: "token"},
			wantErr: "tokens require an HTTPS repository",
		},
		{
			name: "basic on HTTP", repository: "http://example.com/repo",
			creds:   &credsv1.GitCredentials{Username: "user", Password: "password"},
			wantErr: "username/password authentication requires an HTTPS repository",
		},
		{
			name: "userinfo on HTTP", repository: "http://user:secret@example.com/repo",
			wantErr: "the repository URL contains credentials; use an HTTPS repository so they are not sent in clear text",
		},
		{
			name: "token on SSH", repository: "git@example.com:repo",
			creds:   &credsv1.GitCredentials{Token: "token"},
			wantErr: "tokens require an HTTPS repository",
		},
		{
			name: "basic on SSH", repository: "git@example.com:repo",
			creds:   &credsv1.GitCredentials{Username: "user", Password: "password"},
			wantErr: "username/password authentication requires an HTTPS repository",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)

			auth, err := authMethod(ep, tc.creds, Options{})
			if tc.wantErr != "" {
				r.EqualError(err, tc.wantErr)
				r.Nil(auth)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, auth)
		})
	}

	t.Run("default agent", func(t *testing.T) {
		r := require.New(t)
		ep, err := endpoint.Parse("git@example.com:repo")
		r.NoError(err)
		auth, err := authMethod(ep, nil, Options{})
		r.NoError(err)
		r.Nil(auth, "without a host key callback go-git builds the SSH agent auth itself")
	})

	t.Run("missing agent", func(t *testing.T) {
		r := require.New(t)
		t.Setenv("SSH_AUTH_SOCK", "")
		ep, err := endpoint.Parse("git@example.com:repo")
		r.NoError(err)
		auth, err := authMethod(ep, nil, Options{HostKeyCallback: ssh.InsecureIgnoreHostKey()})
		r.ErrorContains(err, "cannot use SSH agent:")
		r.Nil(auth)
	})
}

func TestAuthSSHKeys(t *testing.T) {
	r := require.New(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	r.NoError(err)
	block, err := ssh.MarshalPrivateKey(private, "")
	r.NoError(err)
	keyPEM := string(pem.EncodeToMemory(block))

	for _, tc := range []struct {
		name       string
		repository string
		creds      credsv1.GitCredentials
		wantUser   string
	}{
		{
			name: "inline key over file", repository: "url-user@example.com:repo",
			creds:    credsv1.GitCredentials{PrivateKeyPEM: keyPEM, PrivateKey: "/does/not/exist"},
			wantUser: "url-user",
		},
		{
			name: "key over token", repository: "url-user@example.com:repo",
			creds:    credsv1.GitCredentials{PrivateKeyPEM: keyPEM, Token: "ignored"},
			wantUser: "url-user",
		},
		{
			name: "credential username over URL", repository: "url-user@example.com:repo",
			creds:    credsv1.GitCredentials{PrivateKeyPEM: keyPEM, Username: "credential-user"},
			wantUser: "credential-user",
		},
		{
			name: "URL username", repository: "url-user@example.com:repo",
			creds:    credsv1.GitCredentials{PrivateKeyPEM: keyPEM},
			wantUser: "url-user",
		},
		{
			name: "default username", repository: "ssh://example.com/repo",
			creds:    credsv1.GitCredentials{PrivateKeyPEM: keyPEM},
			wantUser: "git",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)
			sentinel := errors.New("host key rejected by supplied callback")
			callback := func(string, net.Addr, ssh.PublicKey) error { return sentinel }

			auth, err := authMethod(ep, &tc.creds, Options{HostKeyCallback: callback})
			r.NoError(err)
			r.IsType(&gitssh.PublicKeys{}, auth)
			keys := auth.(*gitssh.PublicKeys)
			r.Equal(tc.wantUser, keys.User)
			r.NotNil(keys.Signer)
			r.NotNil(keys.HostKeyCallback)
			r.ErrorIs(keys.HostKeyCallback("example.com:22", nil, keys.Signer.PublicKey()), sentinel)
		})
	}
}
