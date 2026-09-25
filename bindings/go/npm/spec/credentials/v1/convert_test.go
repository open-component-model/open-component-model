package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	v1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertCredentials(t *testing.T) {
	r := require.New(t)

	want := &v1.NPMCredentials{
		Type:     runtime.NewVersionedType(v1.NPMCredentialsType, v1.Version),
		Username: "user",
		Password: "secret",
		Email:    "user@example.com",
		Token:    "token",
	}

	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON([]byte(`{"type":"NPMCredentials/v1","username":"user","password":"secret","email":"user@example.com","token":"token"}`)))

	// the property names are the ones OCM v1 uses for the NpmRegistry consumer
	for _, input := range []runtime.Typed{want, raw, &directv1.DirectCredentials{
		Type: runtime.NewVersionedType(directv1.CredentialsType, directv1.Version),
		Properties: map[string]string{
			"username": "user",
			"password": "secret",
			"email":    "user@example.com",
			"token":    "token",
		},
	}} {
		got, err := v1.ConvertToNPMCredentials(input)
		r.NoError(err)
		r.Equal(want, got)
		r.NotSame(want, got)
	}

	_, err := v1.ConvertToNPMCredentials(&runtime.Raw{Type: runtime.NewUnversionedType("wrong")})
	r.Error(err)
}

func TestSchemeRegistration(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	v1.MustRegisterCredentialType(scheme)

	for _, typ := range []string{"NPMCredentials/v1", "NPMCredentials"} {
		parsed, err := runtime.TypeFromString(typ)
		r.NoError(err)

		obj, err := scheme.NewObject(parsed)
		r.NoError(err)
		r.IsType(&v1.NPMCredentials{}, obj)
	}
}
