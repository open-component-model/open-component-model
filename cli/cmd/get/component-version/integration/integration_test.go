package integration

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	componentversion "ocm.software/open-component-model/cli/cmd/get/component-version"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

func TestGetComponentVersionNormalisedAndHashed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cmd := test.SetupTestCommand(t, componentversion.New)

	require.NoError(t, cmd.Flags().Set(componentversion.FlagOutput, string(componentversion.EncodingNormalisationJSONV4alpha1SHA256)))

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := componentversion.GetComponentVersion(cmd, []string{"ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.27.0"})
	require.NoError(t, err, "getting component version should succeed")

	// TODO this needs to be fixed to the correct hash
	require.Equal(t, buf.String(), "cbaf9f076c31b970cffaf938770fe20f1fec57c731c8198f4b46dd31ee99a4af")
}
