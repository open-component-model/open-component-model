package repository

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

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
