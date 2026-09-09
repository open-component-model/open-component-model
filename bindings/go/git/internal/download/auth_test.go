package download

import (
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/require"

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
}
