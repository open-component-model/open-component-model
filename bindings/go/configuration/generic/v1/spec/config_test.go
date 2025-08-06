package spec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	convert := []byte(`{"type": "generic.config.ocm.software/v1", "configurations": [
{"type": "generic.config.ocm.software/v1", "configurations": [
	{"type": "custom-config", "key": "valuea"}
]}]}`)
	config := &Config{}
	require.NoError(t, json.Unmarshal(convert, config))
	require.Equal(t, "generic.config.ocm.software", config.Type.Name)
	require.Equal(t, "v1", config.Type.Version)
	require.Len(t, config.Configurations, 1)
}
