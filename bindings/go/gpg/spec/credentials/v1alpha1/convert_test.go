package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertToGPGCredentials(t *testing.T) {
	typ := runtime.NewVersionedType(GPGCredentialsType, Version)
	raw := func(typ runtime.Type, data string) *runtime.Raw {
		return &runtime.Raw{Type: typ, Data: []byte(data)}
	}

	tests := []struct {
		name    string
		creds   runtime.Typed
		want    *GPGCredentials
		wantErr string
	}{
		{
			name:  "raw GPGCredentials/v1alpha1",
			creds: raw(typ, `{"type":"GPGCredentials/v1alpha1","privateKeyPGPFile":"/k.asc","passphrase":"pw"}`),
			want:  &GPGCredentials{Type: typ, PrivateKeyPGPFile: "/k.asc", Passphrase: "pw"},
		},
		{
			name:  "raw unversioned GPGCredentials alias",
			creds: raw(runtime.NewUnversionedType(GPGCredentialsType), `{"type":"GPGCredentials","publicKeyPGP":"pub"}`),
			want:  &GPGCredentials{Type: runtime.NewUnversionedType(GPGCredentialsType), PublicKeyPGP: "pub"},
		},
		{
			name: "raw Credentials/v1 maps every property",
			creds: raw(runtime.NewVersionedType(v1.CredentialsType, v1.Version), `{"type":"Credentials/v1","properties":{
				"privateKeyPGP":"priv","privateKeyPGPFile":"/priv.asc","publicKeyPGP":"pub","publicKeyPGPFile":"/pub.asc",
				"passphrase":"pw","keyringFingerprint":"ABCD","keyringHome":"/gnupg","unrelated":"ignored"}}`),
			want: &GPGCredentials{
				Type:               typ,
				PrivateKeyPGP:      "priv",
				PrivateKeyPGPFile:  "/priv.asc",
				PublicKeyPGP:       "pub",
				PublicKeyPGPFile:   "/pub.asc",
				Passphrase:         "pw",
				KeyringFingerprint: "ABCD",
				KeyringHome:        "/gnupg",
			},
		},
		{
			name: "typed DirectCredentials",
			creds: &v1.DirectCredentials{
				Type:       runtime.NewVersionedType(v1.DirectCredentialsType, v1.Version),
				Properties: map[string]string{"keyringFingerprint": "ABCD"},
			},
			want: &GPGCredentials{Type: typ, KeyringFingerprint: "ABCD"},
		},
		{
			name:    "unknown credential type",
			creds:   raw(runtime.NewVersionedType("OCIRegistryCredentials", "v1"), `{"type":"OCIRegistryCredentials/v1"}`),
			wantErr: "error converting credential type",
		},
		{
			name:    "malformed raw data",
			creds:   raw(typ, `{"type":"GPGCredentials/v1alpha1","privateKeyPGP":42}`),
			wantErr: "error converting credential type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := ConvertToGPGCredentials(tt.creds)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, got)
		})
	}

	t.Run("typed GPGCredentials are returned as is", func(t *testing.T) {
		in := &GPGCredentials{Type: typ, PrivateKeyPGP: "priv"}
		got, err := ConvertToGPGCredentials(in)
		require.NoError(t, err)
		require.Same(t, in, got)
	})
}
