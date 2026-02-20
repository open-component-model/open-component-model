package resource

import (
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestCreateRepositoryWithFilesystemConfig(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		filesystemConfig *filesystemv1alpha1.Config
		expectError      bool
	}{
		{
			name: "with filesystem config",
			filesystemConfig: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectError: false,
		},
		{
			name:        "without filesystem config",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &ociv1.Repository{
				BaseUrl: "localhost:5000",
			}
			credentials := map[string]string{}

			repo, err := createRepository(spec, credentials, tt.filesystemConfig, "test")

			if tt.expectError {
				r.Error(err, "expected error")
				r.Nil(repo, "repository should be nil")
			} else {
				r.NoError(err, "should not error")
				r.NotNil(repo, "repository should not be nil")
			}
		})
	}
}
