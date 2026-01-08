package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/looseref"

	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestNewFromCTFRepoV1(t *testing.T) {
	tests := []struct {
		name        string
		repository  *ctfrepospecv1.Repository
		wantErr     bool
		errContains string
	}{
		{
			name: "valid repository with read access",
			repository: &ctfrepospecv1.Repository{
				FilePath:   t.TempDir(),
				AccessMode: ctfrepospecv1.AccessModeReadOnly,
			},
			wantErr: false,
		},
		{
			name: "valid repository with write access",
			repository: &ctfrepospecv1.Repository{
				FilePath:   t.TempDir(),
				AccessMode: ctfrepospecv1.AccessModeReadWrite,
			},
			wantErr: false,
		},
		{
			name: "valid repository with readwrite access",
			repository: &ctfrepospecv1.Repository{
				FilePath:   t.TempDir(),
				AccessMode: ctfrepospecv1.AccessModeReadWrite,
			},
			wantErr: false,
		},
		{
			name: "invalid path",
			repository: &ctfrepospecv1.Repository{
				FilePath:   "/nonexistent/path",
				AccessMode: ctfrepospecv1.AccessModeReadOnly,
			},
			wantErr:     true,
			errContains: "unable to open ctf archive",
		},
		{
			name: "empty path",
			repository: &ctfrepospecv1.Repository{
				FilePath:   "",
				AccessMode: ctfrepospecv1.AccessModeReadOnly,
			},
			wantErr:     true,
			errContains: "a path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewFromCTFRepoV1(t.Context(), tt.repository)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, repo)
		})
	}
}

func TestNewFromOCIRepoV1(t *testing.T) {
	tests := []struct {
		name        string
		repository  *ocirepospecv1.Repository
		wantErr     bool
		errContains string
	}{
		{
			name: "valid repository with http base url",
			repository: &ocirepospecv1.Repository{
				BaseUrl: "http://localhost:5000",
			},
			wantErr: false,
		},
		{
			name: "valid repository with https base url",
			repository: &ocirepospecv1.Repository{
				BaseUrl: "https://registry.example.com",
			},
			wantErr: false,
		},
		{
			name: "empty base url",
			repository: &ocirepospecv1.Repository{
				BaseUrl: "",
			},
			wantErr:     true,
			errContains: "a base url is required",
		},
		{
			name: "base url with path",
			repository: &ocirepospecv1.Repository{
				BaseUrl: "https://registry.example.com/my-org/components",
			},
			wantErr: false,
		},
		{
			name: "base url with path and explicit subPath",
			repository: &ocirepospecv1.Repository{
				BaseUrl: "https://registry.example.com/ignored-path",
				SubPath: "my-org/components",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewFromOCIRepoV1(t.Context(), tt.repository, nil)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, repo)
		})
	}
}

func TestBuildResolver_SubPathExtraction(t *testing.T) {
	tests := []struct {
		name              string
		baseURL           string
		subPath           string
		expectedBasePath  string
		expectedCVRef     string
		expectedReference string // make sure that the reference parser from ORAS isn't breaking.
	}{
		{
			name:              "https with single path segment",
			baseURL:           "https://registry.example.com/my-org",
			subPath:           "",
			expectedBasePath:  "registry.example.com/my-org/component-descriptors",
			expectedCVRef:     "registry.example.com/my-org/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/my-org/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "segmented url",
			baseURL:           "host/ocm/",
			subPath:           "",
			expectedBasePath:  "host/ocm/component-descriptors",
			expectedCVRef:     "host/ocm/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "host/ocm/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "https with multiple path segments",
			baseURL:           "https://registry.example.com/my-org/components",
			subPath:           "",
			expectedBasePath:  "registry.example.com/my-org/components/component-descriptors",
			expectedCVRef:     "registry.example.com/my-org/components/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/my-org/components/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "http with path",
			baseURL:           "http://localhost:5000/test/path",
			subPath:           "",
			expectedBasePath:  "localhost:5000/test/path/component-descriptors",
			expectedCVRef:     "localhost:5000/test/path/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "localhost:5000/test/path/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "no scheme with path",
			baseURL:           "registry.example.com/org/repo",
			subPath:           "",
			expectedBasePath:  "registry.example.com/org/repo/component-descriptors",
			expectedCVRef:     "registry.example.com/org/repo/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/org/repo/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "explicit subPath with path in baseURL",
			baseURL:           "https://registry.example.com/extra",
			subPath:           "explicit/path",
			expectedBasePath:  "registry.example.com/extra/explicit/path/component-descriptors",
			expectedCVRef:     "registry.example.com/extra/explicit/path/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/extra/explicit/path/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "baseURL without path",
			baseURL:           "https://registry.example.com",
			subPath:           "",
			expectedBasePath:  "registry.example.com/component-descriptors",
			expectedCVRef:     "registry.example.com/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "baseURL with port and path",
			baseURL:           "https://registry.example.com:8080/my-org",
			subPath:           "",
			expectedBasePath:  "registry.example.com:8080/my-org/component-descriptors",
			expectedCVRef:     "registry.example.com:8080/my-org/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com:8080/my-org/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "ghcr.io style URL",
			baseURL:           "ghcr.io/my-org/my-repo",
			subPath:           "",
			expectedBasePath:  "ghcr.io/my-org/my-repo/component-descriptors",
			expectedCVRef:     "ghcr.io/my-org/my-repo/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "ghcr.io/my-org/my-repo/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "explicit baseURL and subPath",
			baseURL:           "https://registry.example.com",
			subPath:           "my-org/my-repo",
			expectedBasePath:  "registry.example.com/my-org/my-repo/component-descriptors",
			expectedCVRef:     "registry.example.com/my-org/my-repo/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/my-org/my-repo/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "concrete example for platform-mesh",
			baseURL:           "ghcr.io/platform-mesh",
			subPath:           "",
			expectedBasePath:  "ghcr.io/platform-mesh/component-descriptors",
			expectedCVRef:     "ghcr.io/platform-mesh/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "ghcr.io/platform-mesh/component-descriptors/ocm.software/test-component:1.0.0",
		},
		{
			name:              "avoid duplicates",
			baseURL:           "https://registry.example.com/my-org/components",
			expectedBasePath:  "registry.example.com/my-org/components/component-descriptors",
			expectedCVRef:     "registry.example.com/my-org/components/component-descriptors/ocm.software/test-component:1.0.0",
			expectedReference: "registry.example.com/my-org/components/component-descriptors/ocm.software/test-component:1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &ocirepospecv1.Repository{
				BaseUrl: tt.baseURL,
				SubPath: tt.subPath,
			}

			resolver, err := buildResolver(nil, repository)
			require.NoError(t, err)
			require.NotNil(t, resolver)

			assert.Equal(t, tt.expectedBasePath, resolver.BasePath())
			assert.Equal(t, tt.expectedCVRef, resolver.ComponentVersionReference(t.Context(), "ocm.software/test-component", "1.0.0"))
			stringer, err := looseref.ParseReference(resolver.ComponentVersionReference(t.Context(), "ocm.software/test-component", "1.0.0"))
			require.NoError(t, err)
			assert.Equal(t, tt.expectedReference, stringer.String())
		})
	}
}
