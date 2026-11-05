package registrytest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/internal/testutil/registrytest"
)

func TestHtpasswd(t *testing.T) {
	r := require.New(t)

	line := registrytest.Htpasswd(t, "ocm", "secret")

	user, hash, ok := strings.Cut(line, ":")
	r.True(ok)
	r.Equal("ocm", user)
	r.NoError(bcrypt.CompareHashAndPassword([]byte(hash), []byte("secret")))
	r.Error(bcrypt.CompareHashAndPassword([]byte(hash), []byte("other")))
}

func TestAuthClient(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		wantHost string
	}{
		{name: "host and port", address: "localhost:5000", wantHost: "localhost:5000"},
		{name: "url", address: "http://localhost:5000/v2/", wantHost: "localhost:5000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			client := registrytest.AuthClient(tt.address, "user", "pass")
			r.Equal([]string{"ocm.software/integration-test"}, client.Header["User-Agent"])

			cred, err := client.Credential(t.Context(), tt.wantHost)
			r.NoError(err)
			r.Equal(auth.Credential{Username: "user", Password: "pass"}, cred)
			cred, err = client.Credential(t.Context(), "other:5000")
			r.NoError(err)
			r.Equal(auth.EmptyCredential, cred)
		})
	}

	t.Run("invalid address panics", func(t *testing.T) {
		require.Panics(t, func() { registrytest.AuthClient("http://[::1", "user", "pass") })
	})
}
