package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	configv1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLookupBuiltinPluginConfig_NilConfig(t *testing.T) {
	result, err := LookupBuiltinPluginConfig(nil)
	require.NoError(t, err)

	// Should return defaults
	expected := DefaultBuiltinPluginConfig()
	assert.Equal(t, expected.LogLevel, result.LogLevel)
	assert.Equal(t, expected.LogFormat, result.LogFormat)
}

func TestLookupBuiltinPluginConfig_EmptyConfig(t *testing.T) {
	config := &configv1.Config{
		Type:           runtime.NewVersionedType(configv1.ConfigType, configv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{},
	}

	result, err := LookupBuiltinPluginConfig(config)
	require.NoError(t, err)

	// Should return defaults
	expected := DefaultBuiltinPluginConfig()
	assert.Equal(t, expected.LogLevel, result.LogLevel)
	assert.Equal(t, expected.LogFormat, result.LogFormat)
}

func TestLookupBuiltinPluginConfig_WithBuiltinConfig(t *testing.T) {
	tests := []struct {
		name               string
		configLogLevel     LogLevel
		configLogFormat    LogFormat
		configTempFolder   string
		expectedLogLevel   LogLevel
		expectedLogFormat  LogFormat
		expectedTempFolder string
	}{
		{
			name:               "full config override",
			configLogLevel:     LogLevelError,
			configLogFormat:    LogFormatJSON,
			configTempFolder:   "/tmp/test",
			expectedLogLevel:   LogLevelError,
			expectedLogFormat:  LogFormatJSON,
			expectedTempFolder: "/tmp/test",
		},
		{
			name:               "partial config with defaults",
			configLogLevel:     LogLevelWarn,
			configLogFormat:    "", // empty, should use default
			configTempFolder:   "",
			expectedLogLevel:   LogLevelWarn,
			expectedLogFormat:  LogFormatText, // default
			expectedTempFolder: "",
		},
		{
			name:               "empty config uses all defaults",
			configLogLevel:     "",
			configLogFormat:    "",
			configTempFolder:   "",
			expectedLogLevel:   LogLevelInfo,  // default
			expectedLogFormat:  LogFormatText, // default
			expectedTempFolder: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create builtin config with test values
			builtinConfig := &BuiltinPluginConfig{
				Type:       runtime.NewVersionedType(ConfigType, ConfigTypeV1),
				LogLevel:   tt.configLogLevel,
				LogFormat:  tt.configLogFormat,
				TempFolder: tt.configTempFolder,
			}

			data, err := json.Marshal(builtinConfig)
			require.NoError(t, err)

			config := &configv1.Config{
				Type: runtime.NewVersionedType(configv1.ConfigType, configv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					{
						Type: builtinConfig.GetType(),
						Data: data,
					},
				},
			}

			result, err := LookupBuiltinPluginConfig(config)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedLogLevel, result.LogLevel)
			assert.Equal(t, tt.expectedLogFormat, result.LogFormat)
			assert.Equal(t, tt.expectedTempFolder, result.TempFolder)
		})
	}
}

func TestLookupBuiltinPluginConfig_MultipleConfigs(t *testing.T) {
	// Create first builtin config
	firstConfig := &BuiltinPluginConfig{
		Type:       runtime.NewVersionedType(ConfigType, ConfigTypeV1),
		LogLevel:   LogLevelDebug,
		LogFormat:  LogFormatText,
		TempFolder: "/tmp/first",
	}
	firstData, _ := json.Marshal(firstConfig)

	// Create second builtin config (should take precedence)
	secondConfig := &BuiltinPluginConfig{
		Type:       runtime.NewVersionedType(ConfigType, ConfigTypeV1),
		LogLevel:   LogLevelError,
		LogFormat:  LogFormatJSON,
		TempFolder: "/tmp/second",
	}
	secondData, _ := json.Marshal(secondConfig)

	config := &configv1.Config{
		Type: runtime.NewVersionedType(configv1.ConfigType, configv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{
			{
				Type: firstConfig.GetType(),
				Data: firstData,
			},
			{
				Type: secondConfig.GetType(),
				Data: secondData,
			},
		},
	}

	result, err := LookupBuiltinPluginConfig(config)
	require.NoError(t, err)

	assert.Equal(t, LogLevelError, result.LogLevel)
	assert.Equal(t, LogFormatJSON, result.LogFormat)
	assert.Equal(t, "/tmp/second", result.TempFolder)
}
