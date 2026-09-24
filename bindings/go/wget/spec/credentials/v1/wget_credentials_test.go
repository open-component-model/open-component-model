package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWgetCredentialsValidate(t *testing.T) {
	tests := []struct {
		name        string
		creds       WgetCredentials
		errContains string
	}{
		{
			name:  "basic auth with username and password",
			creds: WgetCredentials{Username: "alice", Password: "secret"},
		},
		{
			name:  "basic auth with username only",
			creds: WgetCredentials{Username: "alice"},
		},
		{
			name:  "bearer token",
			creds: WgetCredentials{IdentityToken: "token"},
		},
		{
			name:  "mTLS with certificate and private key",
			creds: WgetCredentials{Certificate: "cert", PrivateKey: "key"},
		},
		{
			name:  "mTLS with certificate authority",
			creds: WgetCredentials{Certificate: "cert", PrivateKey: "key", CertificateAuthority: "ca"},
		},
		{
			name:  "bearer token combined with mTLS",
			creds: WgetCredentials{IdentityToken: "token", Certificate: "cert", PrivateKey: "key"},
		},
		{
			name:        "empty credentials",
			creds:       WgetCredentials{},
			errContains: "no authentication material",
		},
		{
			name:        "password without username",
			creds:       WgetCredentials{Password: "secret"},
			errContains: "password is set but username is empty",
		},
		{
			name:        "private key without certificate",
			creds:       WgetCredentials{PrivateKey: "key"},
			errContains: "privateKey is set but certificate is empty",
		},
		{
			name:        "certificate without private key",
			creds:       WgetCredentials{Certificate: "cert"},
			errContains: "certificate is set but privateKey is empty",
		},
		{
			name:        "certificate authority without certificate",
			creds:       WgetCredentials{CertificateAuthority: "ca"},
			errContains: "certificateAuthority is set but certificate is empty",
		},
		{
			name:        "certificate authority and private key without certificate",
			creds:       WgetCredentials{CertificateAuthority: "ca", PrivateKey: "key"},
			errContains: "privateKey is set but certificate is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			err := tt.creds.Validate()

			if tt.errContains != "" {
				r.Error(err)
				r.Contains(err.Error(), tt.errContains)
				return
			}
			r.NoError(err)
		})
	}
}
