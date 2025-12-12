package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIdentityFromOCIRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository oci.Repository
		want       runtime.Identity
		wantErr    bool
	}{
		{
			name: "valid OCI repository with default port (https)",
			repository: oci.Repository{
				BaseUrl: "https://registry.example.com/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with default port (oci)",
			repository: oci.Repository{
				BaseUrl: "oci://registry.example.com/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributeScheme:   "oci",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with default port (http)",
			repository: oci.Repository{
				BaseUrl: "http://registry.example.com/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributePort:     "80",
				runtime.IdentityAttributeScheme:   "http",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with explicit port",
			repository: oci.Repository{
				BaseUrl: "https://registry.example.com:443/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with custom port",
			repository: oci.Repository{
				BaseUrl: "https://registry.example.com:5000/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "5000",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with http scheme",
			repository: oci.Repository{
				BaseUrl: "http://registry.example.com:8080/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "8080",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributeScheme:   "http",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with subdomain",
			repository: oci.Repository{
				BaseUrl: "https://docker.registry.example.com/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "docker.registry.example.com",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "valid OCI repository with IP address",
			repository: oci.Repository{
				BaseUrl: "https://192.168.1.100:5000/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "192.168.1.100",
				runtime.IdentityAttributePort:     "5000",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "URL - missing scheme (not absolute)",
			repository: oci.Repository{
				BaseUrl: "registry.example.com/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "URL - missing scheme (localhost)",
			repository: oci.Repository{
				BaseUrl: "localhost:1111/v2",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "localhost",
				runtime.IdentityAttributePath:     "/v2",
				runtime.IdentityAttributePort:     "1111",
				runtime.IdentityAttributeScheme:   "https",
			},
			wantErr: false,
		},
		{
			name: "invalid URL - malformed",
			repository: oci.Repository{
				BaseUrl: "://invalid-url",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid URL - invalid port",
			repository: oci.Repository{
				BaseUrl: "https://registry.example.com:abc/v2",
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IdentityFromOCIRepository(&tt.repository)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, Type, got.GetType())
		})
	}
}

func TestIdentityFromCTFRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository ctf.Repository
		want       runtime.Identity
	}{
		{
			name: "valid CTF repository",
			repository: ctf.Repository{
				FilePath: "/path/to/repo",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType: Type.String(),
				runtime.IdentityAttributePath: "/path/to/repo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IdentityFromCTFRepository(&tt.repository)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, Type, got.GetType())
		})
	}
}
