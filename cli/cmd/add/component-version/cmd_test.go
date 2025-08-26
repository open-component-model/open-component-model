package componentversion

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestGetRepositorySpec(t *testing.T) {
	tests := []struct {
		name           string
		repoPath       string
		expectedType   string
		validateResult func(t *testing.T, result interface{}, repoPath string)
	}{
		{
			name:         "OCI Registry - GitHub Container Registry",
			repoPath:     "ghcr.io/my-org/my-repo",
			expectedType: "*oci.Repository",
			validateResult: func(t *testing.T, result interface{}, repoPath string) {
				repo, ok := result.(*ociv1.Repository)
				require.True(t, ok, "expected *ociv1.Repository")
				require.Equal(t, repoPath, repo.BaseUrl)
			},
		},
		{
			name:         "OCI Registry - localhost with port",
			repoPath:     "localhost:5000/my-repo",
			expectedType: "*oci.Repository",
			validateResult: func(t *testing.T, result interface{}, repoPath string) {
				repo, ok := result.(*ociv1.Repository)
				require.True(t, ok, "expected *ociv1.Repository")
				require.Equal(t, repoPath, repo.BaseUrl)
			},
		},
		{
			name:         "OCI Registry - HTTPS URL",
			repoPath:     "https://registry.example.com/my-repo",
			expectedType: "*oci.Repository",
			validateResult: func(t *testing.T, result interface{}, repoPath string) {
				repo, ok := result.(*ociv1.Repository)
				require.True(t, ok, "expected *ociv1.Repository")
				require.Equal(t, repoPath, repo.BaseUrl)
			},
		},
		{
			name:         "CTF Archive - relative path (non-existing)",
			repoPath:     "./non-existing-archive",
			expectedType: "*ctf.Repository",
			validateResult: func(t *testing.T, result interface{}, repoPath string) {
				repo, ok := result.(*ctfv1.Repository)
				require.True(t, ok, "expected *ctfv1.Repository")
				require.Equal(t, repoPath, repo.Path)
				require.Equal(t, ctfv1.AccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate), repo.AccessMode)
			},
		},
		{
			name:         "CTF Archive - absolute path",
			repoPath:     "/tmp/test-archive",
			expectedType: "*ctf.Repository",
			validateResult: func(t *testing.T, result interface{}, repoPath string) {
				repo, ok := result.(*ctfv1.Repository)
				require.True(t, ok, "expected *ctfv1.Repository")
				require.Equal(t, repoPath, repo.Path)
				require.Equal(t, ctfv1.AccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate), repo.AccessMode)
			},
		},
		{
			name:         "CTF Archive - file URL",
			repoPath:     "file://./local/transport-archive",
			expectedType: "*ctf.Repository",
			validateResult: func(t *testing.T, result interface{}, repoPath string) {
				repo, ok := result.(*ctfv1.Repository)
				require.True(t, ok, "expected *ctfv1.Repository")
				require.Equal(t, repoPath, repo.Path)
				require.Equal(t, ctfv1.AccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate), repo.AccessMode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			cmd := &cobra.Command{Use: "test"}
			cmd.Flags().StringP(FlagRepositoryRef, "r", "default", "repository specification")
			r.NoError(cmd.Flags().Set(FlagRepositoryRef, tt.repoPath))

			result, err := GetRepositorySpec(cmd)
			r.NoError(err, "unexpected error: %v", err)
			r.NotNil(result, "expected non-nil result")

			resultType := getTypeName(result)
			r.Equal(tt.expectedType, resultType, "unexpected repository type")
			tt.validateResult(t, result, tt.repoPath)
		})
	}
}

func TestGetRepositorySpecErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		setupCmd      func(*cobra.Command) error
		expectedError string
	}{
		{
			name: "missing repository flag",
			setupCmd: func(cmd *cobra.Command) error {
				return nil
			},
			expectedError: "getting repository reference flag failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			cmd := &cobra.Command{Use: "test"}
			r.NoError(tt.setupCmd(cmd))

			result, err := GetRepositorySpec(cmd)
			r.Error(err, "expected error but got none")
			r.Nil(result, "expected nil result on error")
			r.Contains(err.Error(), tt.expectedError, "unexpected error message")
		})
	}
}

func getTypeName(v interface{}) string {
	switch v.(type) {
	case *ociv1.Repository:
		return "*oci.Repository"
	case *ctfv1.Repository:
		return "*ctf.Repository"
	default:
		return "unknown"
	}
}
