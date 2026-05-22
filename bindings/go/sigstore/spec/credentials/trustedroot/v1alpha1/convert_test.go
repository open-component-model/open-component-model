package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type fakeTyped struct{}

func (f *fakeTyped) GetType() runtime.Type        { return runtime.NewUnversionedType("Unknown") }
func (f *fakeTyped) SetType(_ runtime.Type)       {}
func (f *fakeTyped) DeepCopyTyped() runtime.Typed { return &fakeTyped{} }

// fakeOIDCTyped simulates the sigstore OIDCIdentityToken credential type without
// importing it (avoids a cycle and keeps this test self-contained). The convertScheme
// must reject any registered or unregistered type that is not TrustedRoot or
// DirectCredentials.
type fakeOIDCTyped struct{}

func (f *fakeOIDCTyped) GetType() runtime.Type {
	return runtime.NewVersionedType("OIDCIdentityToken", "v1alpha1")
}
func (f *fakeOIDCTyped) SetType(_ runtime.Type)       {}
func (f *fakeOIDCTyped) DeepCopyTyped() runtime.Typed { return &fakeOIDCTyped{} }

func TestConvertToTrustedRoot(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *TrustedRoot
		wantErr bool
	}{
		{
			name: "TrustedRoot passthrough",
			input: &TrustedRoot{
				Type:                VersionedType,
				TrustedRootJSON:     `{"keys":[]}`,
				TrustedRootJSONFile: "/path/root.json",
			},
			want: &TrustedRoot{
				Type:                VersionedType,
				TrustedRootJSON:     `{"keys":[]}`,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "DirectCredentials with camelCase keys",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyTrustedRootJSON:     `{"keys":[]}`,
					credentialKeyTrustedRootJSONFile: "/path/root.json",
				},
			},
			want: &TrustedRoot{
				Type:                VersionedType,
				TrustedRootJSON:     `{"keys":[]}`,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: VersionedType,
				Data: []byte(`{"type":"TrustedRoot/v1alpha1","trustedRootJSON":"{\"keys\":[]}","trustedRootJSONFile":"/path/root.json"}`),
			},
			want: &TrustedRoot{
				Type:                VersionedType,
				TrustedRootJSON:     `{"keys":[]}`,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name:    "unknown type returns error",
			input:   &fakeTyped{},
			wantErr: true,
		},
		{
			name:    "OIDCIdentityToken credential rejected",
			input:   &fakeOIDCTyped{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToTrustedRoot(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
