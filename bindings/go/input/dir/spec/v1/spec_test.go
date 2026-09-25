package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDir_Validate(t *testing.T) {
	require.NoError(t, (&Dir{Path: "./some/dir"}).Validate())
	require.ErrorContains(t, (&Dir{}).Validate(), "path is required")
}
