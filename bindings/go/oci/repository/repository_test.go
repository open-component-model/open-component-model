package repository

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

// MockClient is a mock implementation of remote.Client
type MockClient struct {
	mock.Mock
}

func (m *MockClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func (m *MockClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

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
			mockClient := &MockClient{}

			repo, err := NewFromOCIRepoV1(t.Context(), tt.repository, mockClient)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, repo)

			// Verify mock expectations
			mockClient.AssertExpectations(t)
		})
	}
}

func TestNewFromOCIRepoV1_SubPathExtraction(t *testing.T) {
	tests := []struct {
		name                 string
		baseURL              string
		subPath              string
		expectedRegistryHost string // The host part that should be used in HTTP requests
		expectedRepoPath     string // The repository path component that should be used
	}{
		{
			name:                 "https with single path segment",
			baseURL:              "https://registry.example.com/my-org",
			subPath:              "",
			expectedRegistryHost: "registry.example.com",
			expectedRepoPath:     "my-org/component-descriptors/test-component",
		},
		{
			name:                 "https with multiple path segments",
			baseURL:              "https://registry.example.com/my-org/components",
			subPath:              "",
			expectedRegistryHost: "registry.example.com",
			expectedRepoPath:     "my-org/components/component-descriptors/test-component",
		},
		{
			name:                 "http with path",
			baseURL:              "http://localhost:5000/test/path",
			subPath:              "",
			expectedRegistryHost: "localhost:5000",
			expectedRepoPath:     "test/path/component-descriptors/test-component",
		},
		{
			name:                 "no scheme with path",
			baseURL:              "registry.example.com/org/repo",
			subPath:              "",
			expectedRegistryHost: "registry.example.com",
			expectedRepoPath:     "org/repo/component-descriptors/test-component",
		},
		{
			name:                 "explicit subPath overrides path in baseURL",
			baseURL:              "https://registry.example.com/ignored",
			subPath:              "explicit/path",
			expectedRegistryHost: "registry.example.com",
			expectedRepoPath:     "ignored/explicit/path/component-descriptors/test-component",
		},
		{
			name:                 "baseURL without path",
			baseURL:              "https://registry.example.com",
			subPath:              "",
			expectedRegistryHost: "registry.example.com",
			expectedRepoPath:     "component-descriptors/test-component",
		},
		{
			name:                 "baseURL with port and path",
			baseURL:              "https://registry.example.com:8080/my-org",
			subPath:              "",
			expectedRegistryHost: "registry.example.com:8080",
			expectedRepoPath:     "my-org/component-descriptors/test-component",
		},
		{
			name:                 "ghcr.io style URL",
			baseURL:              "ghcr.io/my-org/my-repo",
			subPath:              "",
			expectedRegistryHost: "ghcr.io",
			expectedRepoPath:     "my-org/my-repo/component-descriptors/test-component",
		},
		{
			name:                 "explicit baseURL and subPath",
			baseURL:              "https://registry.example.com",
			subPath:              "my-org/my-repo",
			expectedRegistryHost: "registry.example.com",
			expectedRepoPath:     "my-org/my-repo/component-descriptors/test-component",
		},
		{
			name:                 "concrete example for platform-mesh",
			baseURL:              "ghcr.io/platform-mesh",
			subPath:              "",
			expectedRegistryHost: "ghcr.io",
			expectedRepoPath:     "platform-mesh/component-descriptors/test-component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			var capturedRequests []*http.Request

			// return 404 we don't care about the rest, just the URL.
			mockClient.On("Do", mock.AnythingOfType("*http.Request")).Run(func(args mock.Arguments) {
				req := args.Get(0).(*http.Request)
				capturedRequests = append(capturedRequests, req)
			}).Return(&http.Response{
				StatusCode: 404,
				Body:       http.NoBody,
			}, nil)

			repository := &ocirepospecv1.Repository{
				BaseUrl: tt.baseURL,
				SubPath: tt.subPath,
			}

			repo, err := NewFromOCIRepoV1(t.Context(), repository, mockClient)
			require.NoError(t, err)
			require.NotNil(t, repo)

			_, _ = repo.GetComponentVersion(t.Context(), "test-component", "1.0.0")
			require.NotEmpty(t, capturedRequests, "Expected at least one HTTP request")

			var manifestRequest *http.Request
			for _, req := range capturedRequests {
				if req.URL.Path != "/v2/" && req.URL.Path != "" {
					manifestRequest = req
					break
				}
			}

			if manifestRequest != nil {
				assert.Equal(t, tt.expectedRegistryHost, manifestRequest.URL.Host)
				assert.Contains(t, manifestRequest.URL.Path, tt.expectedRepoPath)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestNewFromOCIRepoV1_NoDuplicatePath(t *testing.T) {
	mockClient := &MockClient{}
	var capturedRequests []*http.Request

	// again, just return 404
	mockClient.On("Do", mock.AnythingOfType("*http.Request")).Run(func(args mock.Arguments) {
		req := args.Get(0).(*http.Request)
		capturedRequests = append(capturedRequests, req)
	}).Return(&http.Response{
		StatusCode: 404,
		Body:       http.NoBody,
	}, nil)

	repository := &ocirepospecv1.Repository{
		BaseUrl: "https://registry.example.com/my-org/components",
		SubPath: "", // this is the thing we are testing
	}

	repo, err := NewFromOCIRepoV1(t.Context(), repository, mockClient)
	require.NoError(t, err)

	_, _ = repo.GetComponentVersion(t.Context(), "test-component", "1.0.0")
	var manifestRequest *http.Request
	for _, req := range capturedRequests {
		if req.URL.Path != "/v2/" && req.URL.Path != "" {
			manifestRequest = req
			break
		}
	}

	require.NotNil(t, manifestRequest)
	assert.Equal(t, "registry.example.com", manifestRequest.URL.Host)
	expectedPath := "/v2/my-org/components/component-descriptors/test-component"
	assert.Contains(t, manifestRequest.URL.Path, expectedPath)

	// check for duplicates
	assert.NotContains(t, manifestRequest.URL.Path, "/my-org/components/my-org/components")
}
