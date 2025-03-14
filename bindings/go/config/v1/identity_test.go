package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseURLToIdentity(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    Identity
		wantErr bool
	}{
		{
			uri: "http://docker.io",
			want: Identity{
				IdentityAttributeHostname: "docker.io",
				IdentityAttributeScheme:   "http",
			},
		},
		{
			uri: "https://docker.io",
			want: Identity{
				IdentityAttributeHostname: "docker.io",
				IdentityAttributeScheme:   "https",
			},
		},
		{
			uri: "docker.io",
			want: Identity{
				IdentityAttributeHostname: "docker.io",
			},
		},
		{
			uri: "my-registry.io:5000",
			want: Identity{
				IdentityAttributeHostname: "my-registry.io",
				IdentityAttributePort:     "5000",
			},
		},
		{
			uri: "my-registry.io:5000/path",
			want: Identity{
				IdentityAttributeHostname: "my-registry.io",
				IdentityAttributePort:     "5000",
				IdentityAttributePath:     "path",
			},
		},
		{
			uri: "localhost:8080",
			want: Identity{
				IdentityAttributeHostname: "localhost",
				IdentityAttributePort:     "8080",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			r := require.New(t)
			got, err := ParseURLToIdentity(tt.uri)
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Truef(tt.want.Equal(got), "expected %v to be equal to %v", tt.want, got)
		})
	}
}
