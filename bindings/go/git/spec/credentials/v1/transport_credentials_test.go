package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	credentials "ocm.software/open-component-model/bindings/go/git/spec/credentials"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestTransportCredentials(t *testing.T) {
	for _, tc := range []struct {
		name    string
		json    string
		want    runtime.Typed
		wantErr string
	}{
		{name: "basic token as password", json: `{"type":"GitHTTPSCredentials/v1","username":"user","password":"access-token"}`, want: &v1.GitHTTPSCredentials{Type: runtime.NewVersionedType(v1.GitHTTPSCredentialsType, v1.Version), Username: "user", Password: "access-token"}},
		{name: "basic empty password", json: `{"type":"GitHTTPSCredentials/v1","username":"user","password":""}`, want: &v1.GitHTTPSCredentials{Type: runtime.NewVersionedType(v1.GitHTTPSCredentialsType, v1.Version), Username: "user"}},
		{name: "bearer", json: `{"type":"GitBearerCredentials/v1","token":"access-token"}`, want: &v1.GitBearerCredentials{Type: runtime.NewVersionedType(v1.GitBearerCredentialsType, v1.Version), Token: "access-token"}},
		{name: "SSH file", json: `{"type":"GitSSHCredentials/v1","privateKey":"/keys/key","passphrase":"phrase"}`, want: &v1.GitSSHCredentials{Type: runtime.NewVersionedType(v1.GitSSHCredentialsType, v1.Version), PrivateKey: "/keys/key", Passphrase: "phrase"}},
		{name: "SSH agent", json: `{"type":"GitSSHCredentials/v1","username":"ssh-user"}`, want: &v1.GitSSHCredentials{Type: runtime.NewVersionedType(v1.GitSSHCredentialsType, v1.Version), Username: "ssh-user"}},
		{name: "basic missing username", json: `{"type":"GitHTTPSCredentials/v1","password":"secret"}`, wantErr: "require a username"},
		{name: "basic colon username", json: `{"type":"GitHTTPSCredentials/v1","username":"user:other","password":"secret"}`, wantErr: "must not contain a colon"},
		{name: "bearer missing token", json: `{"type":"GitBearerCredentials/v1"}`, wantErr: "requires a token"},
		{name: "SSH conflicting keys", json: `{"type":"GitSSHCredentials/v1","privateKey":"key","privateKeyPEM":"pem"}`, wantErr: "only one"},
		{name: "SSH passphrase without key", json: `{"type":"GitSSHCredentials/v1","passphrase":"phrase"}`, wantErr: "requires a private key"},
		{name: "basic mixed bearer", json: `{"type":"GitHTTPSCredentials/v1","username":"user","password":"secret","token":"secret"}`, wantErr: `unknown field "token"`},
		{name: "bearer mixed basic", json: `{"type":"GitBearerCredentials/v1","token":"secret","username":"user"}`, wantErr: `unknown field "username"`},
		{name: "SSH HTTP password", json: `{"type":"GitSSHCredentials/v1","privateKey":"key","password":"secret"}`, wantErr: `unknown field "password"`},
		{name: "unsupported", json: `{"type":"GitHTTPSCredentials/v2"}`, wantErr: "unsupported git credential type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			raw := &runtime.Raw{}
			r.NoError(raw.UnmarshalJSON([]byte(tc.json)))
			got, err := v1.ConvertCredentials(raw)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				r.Nil(got)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
			r.True(credentials.Scheme.IsRegistered(got.GetType()))
			got, err = v1.ConvertCredentials(tc.want)
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
