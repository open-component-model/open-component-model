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

func TestConvertToTSACredentials(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *TSACredentials
		wantErr bool
	}{
		{
			name: "TSACredentials passthrough",
			input: &TSACredentials{
				Type:             VersionedType,
				RootCertsPEM:     "inline-pem",
				RootCertsPEMFile: "/path/root.pem",
			},
			want: &TSACredentials{
				Type:             VersionedType,
				RootCertsPEM:     "inline-pem",
				RootCertsPEMFile: "/path/root.pem",
			},
		},
		{
			name: "DirectCredentials camelCase",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
				Properties: map[string]string{
					"rootCertsPEM":     "inline-pem",
					"rootCertsPEMFile": "/path/root.pem",
				},
			},
			want: &TSACredentials{
				Type:             VersionedType,
				RootCertsPEM:     "inline-pem",
				RootCertsPEMFile: "/path/root.pem",
			},
		},
		{
			name: "DirectCredentials deprecated snake_case",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
				Properties: map[string]string{
					"root_certs_pem":      "inline-pem",
					"root_certs_pem_file": "/path/root.pem",
				},
			},
			want: &TSACredentials{
				Type:             VersionedType,
				RootCertsPEM:     "inline-pem",
				RootCertsPEMFile: "/path/root.pem",
			},
		},
		{
			name: "camelCase wins over deprecated snake_case",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
				Properties: map[string]string{
					"rootCertsPEMFile":    "/preferred.pem",
					"root_certs_pem_file": "/legacy.pem",
				},
			},
			want: &TSACredentials{
				Type:             VersionedType,
				RootCertsPEMFile: "/preferred.pem",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: VersionedType,
				Data: []byte(`{"type":"TSACredentials/v1alpha1","rootCertsPEMFile":"/path/root.pem"}`),
			},
			want: &TSACredentials{
				Type:             VersionedType,
				RootCertsPEMFile: "/path/root.pem",
			},
		},
		{
			name:  "nil returns empty typed credentials",
			input: nil,
			want:  &TSACredentials{Type: VersionedType},
		},
		{
			name:    "unknown type returns error",
			input:   &fakeTyped{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := ConvertToTSACredentials(tt.input)
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			assert.Equal(t, tt.want, got)
		})
	}
}
