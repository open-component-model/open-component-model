package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHelm_Validate(t *testing.T) {
	require.NoError(t, (&Helm{Path: "./charts/mychart"}).Validate())
	require.NoError(t, (&Helm{HelmRepository: "https://charts.example.com"}).Validate())
	require.ErrorContains(t, (&Helm{}).Validate(), "either path or helmRepository must be specified")
	require.ErrorContains(t,
		(&Helm{Path: "./charts/mychart", HelmRepository: "https://charts.example.com"}).Validate(),
		"only one of path or helmRepository can be specified")
}
