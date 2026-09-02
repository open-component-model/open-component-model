package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFile_Validate(t *testing.T) {
	require.NoError(t, (&File{Path: "./some/file.txt"}).Validate())
	require.ErrorContains(t, (&File{}).Validate(), "path is required")
}
