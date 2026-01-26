package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLookupCredentialConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         *genericv1.Config
		expectedConfig func(t *testing.T, config *Config)
		expectedErr    string
	}{
		{
			name:   "nil config returns nil for config",
			config: nil,
			expectedConfig: func(t *testing.T, config *Config) {
				assert.Nil(t, config)
			},
		},
		{
			name: "empty configurations returns empty config",
			config: &genericv1.Config{
				Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.Nil(t, config)
			},
		},
		{
			name: "no matching credential configs returns nil",
			config: &genericv1.Config{
				Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					mustCreateRaw(t, map[string]any{
						"type": "some.other.config/v1",
						"data": "value",
					}),
				},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.Nil(t, config)
			},
		},
		{
			name: "single versioned credential config",
			config: &genericv1.Config{
				Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					mustCreateRaw(t, map[string]any{
						"type": "credentials.config.ocm.software/v1",
						"consumers": []map[string]any{
							{
								"identities": []map[string]string{
									{"type": "test-identity"},
								},
								"credentials": []map[string]any{
									{"type": "Credentials/v1", "properties": map[string]string{"username": "user1"}},
								},
							},
						},
					}),
				},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.NotNil(t, config)
				assert.Len(t, config.Consumers, 1)
				assert.Len(t, config.Consumers[0].Identities, 1)
				assert.Equal(t, "test-identity", config.Consumers[0].Identities[0]["type"])
			},
		},
		{
			name: "single unversioned credential config",
			config: &genericv1.Config{
				Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					mustCreateRaw(t, map[string]any{
						"type": "credentials.config.ocm.software",
						"consumers": []map[string]any{
							{
								"identities": []map[string]string{
									{"type": "unversioned-identity"},
								},
								"credentials": []map[string]any{},
							},
						},
					}),
				},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.NotNil(t, config)
				assert.Len(t, config.Consumers, 1)
				assert.Equal(t, "unversioned-identity", config.Consumers[0].Identities[0]["type"])
			},
		},
		{
			name: "multiple credential configs are merged",
			config: &genericv1.Config{
				Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					mustCreateRaw(t, map[string]any{
						"type": "credentials.config.ocm.software/v1",
						"consumers": []map[string]any{
							{
								"identities": []map[string]string{
									{"type": "identity-1"},
								},
								"credentials": []map[string]any{},
							},
						},
					}),
					mustCreateRaw(t, map[string]any{
						"type": "credentials.config.ocm.software/v1",
						"consumers": []map[string]any{
							{
								"identities": []map[string]string{
									{"type": "identity-2"},
								},
								"credentials": []map[string]any{},
							},
						},
					}),
				},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.NotNil(t, config)
				assert.Len(t, config.Consumers, 2)
				assert.Equal(t, "identity-1", config.Consumers[0].Identities[0]["type"])
				assert.Equal(t, "identity-2", config.Consumers[1].Identities[0]["type"])
			},
		},
		{
			name: "mixed config types filters only credentials",
			config: &genericv1.Config{
				Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					mustCreateRaw(t, map[string]any{
						"type": "some.other.config/v1",
						"data": "ignored",
					}),
					mustCreateRaw(t, map[string]any{
						"type": "credentials.config.ocm.software/v1",
						"consumers": []map[string]any{
							{
								"identities": []map[string]string{
									{"type": "filtered-identity"},
								},
								"credentials": []map[string]any{},
							},
						},
					}),
					mustCreateRaw(t, map[string]any{
						"type":  "another.config.type",
						"value": "also-ignored",
					}),
				},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.NotNil(t, config)
				assert.Len(t, config.Consumers, 1)
				assert.Equal(t, "filtered-identity", config.Consumers[0].Identities[0]["type"])
			},
		},
		{
			name: "credential config with repositories",
			config: &genericv1.Config{
				Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
				Configurations: []*runtime.Raw{
					mustCreateRaw(t, map[string]any{
						"type": "credentials.config.ocm.software/v1",
						"repositories": []map[string]any{
							{"repository": map[string]any{"type": "DockerConfig/v1", "path": "~/.docker/config.json"}},
						},
						"consumers": []map[string]any{},
					}),
				},
			},
			expectedConfig: func(t *testing.T, config *Config) {
				assert.NotNil(t, config)
				assert.Len(t, config.Repositories, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LookupCredentialConfig(tt.config)
			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				return
			}
			require.NoError(t, err)
			tt.expectedConfig(t, result)
		})
	}
}

func mustCreateRaw(t *testing.T, data map[string]any) *runtime.Raw {
	t.Helper()
	bytes, err := json.Marshal(data)
	require.NoError(t, err)

	raw := &runtime.Raw{}
	require.NoError(t, raw.UnmarshalJSON(bytes))
	return raw
}
