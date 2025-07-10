package v1

import (
	"encoding/json"
	"testing"

	configv1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLookupBuiltinPluginConfig_NilConfig(t *testing.T) {
	result, err := LookupBuiltinPluginConfig(nil)
	if err != nil {
		t.Fatalf("LookupBuiltinPluginConfig() error = %v", err)
	}
	
	// Should return defaults
	expected := DefaultBuiltinPluginConfig()
	if result.LogLevel != expected.LogLevel {
		t.Errorf("Expected LogLevel %v, got %v", expected.LogLevel, result.LogLevel)
	}
	if result.LogFormat != expected.LogFormat {
		t.Errorf("Expected LogFormat %v, got %v", expected.LogFormat, result.LogFormat)
	}
}

func TestLookupBuiltinPluginConfig_EmptyConfig(t *testing.T) {
	config := &configv1.Config{
		Type: runtime.NewVersionedType(configv1.ConfigType, configv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{},
	}
	
	result, err := LookupBuiltinPluginConfig(config)
	if err != nil {
		t.Fatalf("LookupBuiltinPluginConfig() error = %v", err)
	}
	
	// Should return defaults
	expected := DefaultBuiltinPluginConfig()
	if result.LogLevel != expected.LogLevel {
		t.Errorf("Expected LogLevel %v, got %v", expected.LogLevel, result.LogLevel)
	}
	if result.LogFormat != expected.LogFormat {
		t.Errorf("Expected LogFormat %v, got %v", expected.LogFormat, result.LogFormat)
	}
}

func TestLookupBuiltinPluginConfig_WithBuiltinConfig(t *testing.T) {
	tests := []struct {
		name           string
		configLogLevel LogLevel
		configLogFormat LogFormat
		configTempFolder string
		expectedLogLevel LogLevel
		expectedLogFormat LogFormat
		expectedTempFolder string
	}{
		{
			name:             "full config override",
			configLogLevel:   LogLevelError,
			configLogFormat:  LogFormatJSON,
			configTempFolder: "/tmp/test",
			expectedLogLevel: LogLevelError,
			expectedLogFormat: LogFormatJSON,
			expectedTempFolder: "/tmp/test",
		},
		{
			name:             "partial config with defaults",
			configLogLevel:   LogLevelWarn,
			configLogFormat:  "", // empty, should use default
			configTempFolder: "",
			expectedLogLevel: LogLevelWarn,
			expectedLogFormat: LogFormatText, // default
			expectedTempFolder: "",
		},
		{
			name:             "empty config uses all defaults",
			configLogLevel:   "",
			configLogFormat:  "",
			configTempFolder: "",
			expectedLogLevel: LogLevelInfo, // default
			expectedLogFormat: LogFormatText, // default
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
			if err != nil {
				t.Fatalf("Failed to marshal builtin config: %v", err)
			}
			
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
			if err != nil {
				t.Fatalf("LookupBuiltinPluginConfig() error = %v", err)
			}
			
			if result.LogLevel != tt.expectedLogLevel {
				t.Errorf("Expected LogLevel %v, got %v", tt.expectedLogLevel, result.LogLevel)
			}
			if result.LogFormat != tt.expectedLogFormat {
				t.Errorf("Expected LogFormat %v, got %v", tt.expectedLogFormat, result.LogFormat)
			}
			if result.TempFolder != tt.expectedTempFolder {
				t.Errorf("Expected TempFolder %v, got %v", tt.expectedTempFolder, result.TempFolder)
			}
		})
	}
}

func TestLookupBuiltinPluginConfig_MultipleConfigs(t *testing.T) {
	// Create first builtin config
	firstConfig := &BuiltinPluginConfig{
		Type:      runtime.NewVersionedType(ConfigType, ConfigTypeV1),
		LogLevel:  LogLevelDebug,
		LogFormat: LogFormatText,
		TempFolder: "/tmp/first",
	}
	firstData, _ := json.Marshal(firstConfig)
	
	// Create second builtin config (should take precedence)
	secondConfig := &BuiltinPluginConfig{
		Type:      runtime.NewVersionedType(ConfigType, ConfigTypeV1),
		LogLevel:  LogLevelError,
		LogFormat: LogFormatJSON,
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
	if err != nil {
		t.Fatalf("LookupBuiltinPluginConfig() error = %v", err)
	}
	
	// Should use values from the last (second) config
	if result.LogLevel != LogLevelError {
		t.Errorf("Expected LogLevel from second config (error), got %v", result.LogLevel)
	}
	if result.LogFormat != LogFormatJSON {
		t.Errorf("Expected LogFormat from second config (json), got %v", result.LogFormat)
	}
	if result.TempFolder != "/tmp/second" {
		t.Errorf("Expected TempFolder from second config (/tmp/second), got %v", result.TempFolder)
	}
}

func TestDefaultBuiltinPluginConfig(t *testing.T) {
	defaults := DefaultBuiltinPluginConfig()
	
	if defaults.LogLevel != LogLevelInfo {
		t.Errorf("Expected default LogLevel to be info, got %v", defaults.LogLevel)
	}
	if defaults.LogFormat != LogFormatText {
		t.Errorf("Expected default LogFormat to be text, got %v", defaults.LogFormat)
	}
	if defaults.TempFolder != "" {
		t.Errorf("Expected default TempFolder to be empty, got %v", defaults.TempFolder)
	}
	expectedType := runtime.NewVersionedType(ConfigType, ConfigTypeV1).String()
	if defaults.Type.String() != expectedType {
		t.Errorf("Expected default Type to be %s, got %v", expectedType, defaults.Type.String())
	}
}

func TestBuiltinPluginConfig_GetLogLevel(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{"invalid", "INFO"}, // should default to INFO
	}
	
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			config := &BuiltinPluginConfig{LogLevel: tt.level}
			result := config.GetLogLevel()
			if result.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}