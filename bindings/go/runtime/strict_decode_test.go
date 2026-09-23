package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type strictDecodeCredentials struct {
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
}

func (c *strictDecodeCredentials) GetType() runtime.Type  { return c.Type }
func (c *strictDecodeCredentials) SetType(t runtime.Type) { c.Type = t }
func (c *strictDecodeCredentials) DeepCopyTyped() runtime.Typed {
	cp := *c
	return &cp
}

var strictDecodeCredentialsType = runtime.NewVersionedType("StrictDecodeCredentials", "v1")

func TestDecodeStrict(t *testing.T) {
	tests := []struct {
		name        string
		from        runtime.Typed
		want        *strictDecodeCredentials
		errContains string
	}{
		{
			name: "valid raw round trip",
			from: &runtime.Raw{
				Type: strictDecodeCredentialsType,
				Data: []byte(`{"type":"StrictDecodeCredentials/v1","username":"alice","password":"secret"}`),
			},
			want: &strictDecodeCredentials{
				Type:     strictDecodeCredentialsType,
				Username: "alice",
				Password: "secret",
			},
		},
		{
			name: "valid typed source round trip",
			from: &strictDecodeCredentials{
				Type:     strictDecodeCredentialsType,
				Username: "alice",
				Password: "secret",
			},
			want: &strictDecodeCredentials{
				Type:     strictDecodeCredentialsType,
				Username: "alice",
				Password: "secret",
			},
		},
		{
			name: "unknown field is rejected",
			from: &runtime.Raw{
				Type: strictDecodeCredentialsType,
				Data: []byte(`{"type":"StrictDecodeCredentials/v1","properties":{"username":"alice"}}`),
			},
			errContains: `unknown field "properties"`,
		},
		{
			name: "wrong field type is rejected",
			from: &runtime.Raw{
				Type: strictDecodeCredentialsType,
				Data: []byte(`{"type":"StrictDecodeCredentials/v1","username":42}`),
			},
			errContains: "cannot unmarshal number into Go struct field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			into := &strictDecodeCredentials{}
			err := runtime.DecodeStrict(tt.from, into)

			if tt.errContains != "" {
				r.Error(err)
				r.Contains(err.Error(), tt.errContains)
				r.Contains(err.Error(), strictDecodeCredentialsType.String())
				return
			}
			r.NoError(err)
			r.Equal(tt.want, into)
		})
	}
}
