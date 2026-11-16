package types

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestConfigIdleTimeoutRoundTrip pins the wire format of Config.IdleTimeout.
// The plugin configuration travels between the manager and the plugin binary as
// JSON, and encoding/json v1 wrote a time.Duration as an integer number of
// nanoseconds, so Duration has to keep producing and accepting exactly that.
func TestConfigIdleTimeoutRoundTrip(t *testing.T) {
	idleTimeout := Duration(90 * time.Second)
	config := Config{
		ID:          "test-plugin",
		Type:        Socket,
		PluginType:  PluginType("TestPlugin"),
		IdleTimeout: &idleTimeout,
	}

	data, err := json.Marshal(config, runtime.JSONOptions)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"test-plugin","type":"unix","pluginType":"TestPlugin","idleTimeout":90000000000}`, string(data))

	var decoded Config
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.IdleTimeout)
	require.Equal(t, 90*time.Second, time.Duration(*decoded.IdleTimeout))
	require.Equal(t, config, decoded)
}

// TestConfigIdleTimeoutAbsent verifies that an unset idle timeout is omitted and
// decodes back to nil, which is what makes the plugin fall back to its default.
func TestConfigIdleTimeoutAbsent(t *testing.T) {
	data, err := json.Marshal(Config{ID: "test-plugin", Type: Socket}, runtime.JSONOptions)
	require.NoError(t, err)
	require.NotContains(t, string(data), "idleTimeout")

	var decoded Config
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Nil(t, decoded.IdleTimeout)
}
