package v1

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
				Type:                TrustedRootVersionedType,
				TrustedRootJSONFile: "/path/root.json",
			},
			want: &TrustedRoot{
				Type:                TrustedRootVersionedType,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					CredentialKeyTrustedRootJSONFile: "/path/root.json",
				},
			},
			want: &TrustedRoot{
				Type:                TrustedRootVersionedType,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "DirectCredentials with deprecated snake_case keys",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					DeprecatedCredentialKeyTrustedRootJSONFile: "/path/root.json",
				},
			},
			want: &TrustedRoot{
				Type:                TrustedRootVersionedType,
				TrustedRootJSONFile: "/path/root.json",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: TrustedRootVersionedType,
				Data: []byte(`{"type":"TrustedRoot/v1","trustedRootJSONFile":"/path/root.json"}`),
			},
			want: &TrustedRoot{
				Type:                TrustedRootVersionedType,
				TrustedRootJSONFile: "/path/root.json",
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
