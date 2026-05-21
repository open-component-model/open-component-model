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

func TestConvertToSigstoreCredentials(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *SigstoreCredentials
		wantErr bool
	}{
		{
			name: "SigstoreCredentials passthrough",
			input: &SigstoreCredentials{
				Type:      SigstoreCredentialsVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
			want: &SigstoreCredentials{
				Type:      SigstoreCredentialsVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:     "test-token",
					credentialKeyTokenFile: "/path/token",
				},
			},
			want: &SigstoreCredentials{
				Type:      SigstoreCredentialsVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "DirectCredentials with deprecated snake_case tokenFile key",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:               "test-token",
					deprecatedCredentialKeyTokenFile: "/path/token",
				},
			},
			want: &SigstoreCredentials{
				Type:      SigstoreCredentialsVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "DirectCredentials with trustedRoot fields",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:               "test-token",
					credentialKeyTrustedRootJSON:     `{"keys":[]}`,
					credentialKeyTrustedRootJSONFile: "/path/root.json",
				},
			},
			want: &SigstoreCredentials{
				Type:                SigstoreCredentialsVersionedType,
				Token:               "test-token",
				TrustedRootJSON:     `{"keys":[]}`,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "DirectCredentials with deprecated trustedRoot keys",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:                         "test-token",
					deprecatedCredentialKeyTrustedRootJSON:     `{"keys":[]}`,
					deprecatedCredentialKeyTrustedRootJSONFile: "/path/root.json",
				},
			},
			want: &SigstoreCredentials{
				Type:                SigstoreCredentialsVersionedType,
				Token:               "test-token",
				TrustedRootJSON:     `{"keys":[]}`,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: SigstoreCredentialsVersionedType,
				Data: []byte(`{"type":"SigstoreCredentials/v1alpha1","token":"test-token","tokenFile":"/path/token"}`),
			},
			want: &SigstoreCredentials{
				Type:      SigstoreCredentialsVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name:    "unknown type returns error",
			input:   &fakeTyped{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToSigstoreCredentials(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
