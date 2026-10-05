package v1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertCredentials(t *testing.T) {
	r := require.New(t)

	want := &v1.GitCredentials{
		Type:          runtime.NewVersionedType(v1.GitCredentialsType, v1.Version),
		Username:      "git",
		Password:      "passphrase",
		Token:         "token",
		PrivateKey:    "/keys/key",
		PrivateKeyPEM: "pem",
	}

	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON([]byte(`{"type":"GitCredentials/v1","username":"git","password":"passphrase","token":"token","privateKey":"/keys/key","privateKeyPEM":"pem"}`)))

	for _, input := range []runtime.Typed{want, raw, &directv1.DirectCredentials{
		Type: runtime.NewVersionedType(directv1.CredentialsType, directv1.Version),
		Properties: map[string]string{
			"username":      "git",
			"password":      "passphrase",
			"token":         "token",
			"privateKey":    "/keys/key",
			"privateKeyPEM": "pem",
		},
	}} {
		got, err := v1.ConvertToGitCredentials(input)
		r.NoError(err)
		r.Equal(want, got)
		r.NotSame(want, got)
	}

	_, err := v1.ConvertToGitCredentials(&runtime.Raw{Type: runtime.NewUnversionedType("wrong")})
	r.Error(err)
}

func TestConvertLegacyCredentialsToTransport(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credentials v1.GitCredentials
		want        runtime.Typed
		wantErr     string
	}{
		{name: "inline key over file and token", credentials: v1.GitCredentials{Username: "git", Password: "phrase", Token: "ignored", PrivateKey: "/ignored", PrivateKeyPEM: "pem"}, want: &v1.GitSSHCredentials{Type: runtime.NewVersionedType(v1.GitSSHCredentialsType, v1.Version), Username: "git", PrivateKeyPEM: "pem", Passphrase: "phrase"}},
		{name: "file key over token", credentials: v1.GitCredentials{Password: "phrase", PrivateKey: "/keys/key", Token: "ignored"}, want: &v1.GitSSHCredentials{Type: runtime.NewVersionedType(v1.GitSSHCredentialsType, v1.Version), PrivateKey: "/keys/key", Passphrase: "phrase"}},
		{name: "token over basic", credentials: v1.GitCredentials{Username: "ignored", Password: "ignored", Token: "bearer"}, want: &v1.GitBearerCredentials{Type: runtime.NewVersionedType(v1.GitBearerCredentialsType, v1.Version), Token: "bearer"}},
		{name: "basic", credentials: v1.GitCredentials{Username: "user", Password: "access-token"}, want: &v1.GitBasicCredentials{Type: runtime.NewVersionedType(v1.GitBasicCredentialsType, v1.Version), Username: "user", Password: "access-token"}},
		{name: "username only", credentials: v1.GitCredentials{Username: "user"}, want: &v1.GitBasicCredentials{Type: runtime.NewVersionedType(v1.GitBasicCredentialsType, v1.Version), Username: "user"}},
		{name: "empty", credentials: v1.GitCredentials{}},
		{name: "password without username or key", credentials: v1.GitCredentials{Password: "phrase"}, wantErr: "password requires a username or SSH private key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			tc.credentials.Type = runtime.NewVersionedType(v1.GitCredentialsType, v1.Version)
			data, err := json.Marshal(&tc.credentials)
			r.NoError(err)
			raw := &runtime.Raw{}
			r.NoError(raw.UnmarshalJSON(data))
			direct := &directv1.DirectCredentials{Type: runtime.NewVersionedType(directv1.CredentialsType, directv1.Version), Properties: map[string]string{
				"username": tc.credentials.Username, "password": tc.credentials.Password, "token": tc.credentials.Token,
				"privateKey": tc.credentials.PrivateKey, "privateKeyPEM": tc.credentials.PrivateKeyPEM,
			}}
			for _, input := range []runtime.Typed{&tc.credentials, raw, direct} {
				got, err := v1.ConvertCredentials(input)
				if tc.wantErr != "" {
					r.EqualError(err, tc.wantErr)
					r.Nil(got)
					continue
				}
				r.NoError(err)
				r.Equal(tc.want, got)
			}
		})
	}
}
