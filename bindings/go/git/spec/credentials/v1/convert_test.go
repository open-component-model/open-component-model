package v1_test

import (
	"github.com/stretchr/testify/require"
	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"testing"
)

func TestConvertCredentials(t *testing.T) {
	r := require.New(t)

	want := &v1.GitCredentials{
		Type:       runtime.NewVersionedType(v1.GitCredentialsType, v1.Version),
		Username:   "git",
		Password:   "passphrase",
		Token:      "token",
		PrivateKey: "/keys/key",
	}

	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON([]byte(`{"type":"GitCredentials/v1","username":"git","password":"passphrase","token":"token","privateKey":"/keys/key"}`)))

	for _, input := range []runtime.Typed{want, raw, &directv1.DirectCredentials{
		Type: runtime.NewVersionedType(directv1.CredentialsType, directv1.Version),
		Properties: map[string]string{
			"username":   "git",
			"password":   "passphrase",
			"token":      "token",
			"privateKey": "/keys/key",
		},
	}} {
		got, err := v1.ConvertToGitCredentials(input)
		r.NoError(err)
		r.Equal(want, got)
		r.NotSame(want, got)
	}

	got, err := v1.ConvertToGitCredentials(nil)
	r.NoError(err)
	r.Nil(got)
	_, err = v1.ConvertToGitCredentials(&runtime.Raw{Type: runtime.NewUnversionedType("wrong")})
	r.Error(err)
}
