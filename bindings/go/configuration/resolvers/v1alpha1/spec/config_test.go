package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// makeGenericConfig creates a genericv1.Config from a list of raw JSON configuration entries.
func makeGenericConfig(t *testing.T, entries ...string) *genericv1.Config {
	t.Helper()
	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: make([]*runtime.Raw, 0, len(entries)),
	}
	for _, entry := range entries {
		raw := &runtime.Raw{}
		require.NoError(t, json.Unmarshal([]byte(entry), raw))
		cfg.Configurations = append(cfg.Configurations, raw)
	}
	return cfg
}

func TestLookup_BackwardCompatibility_NoVersionConstraint(t *testing.T) {
	// Old config without versionConstraint must still deserialize correctly.
	generic := makeGenericConfig(t, `{
		"type": "resolvers.config.ocm.software/v1alpha1",
		"resolvers": [
			{
				"componentNamePattern": "ocm.software/core/*",
				"repository": {
					"type": "OCIRepository/v1",
					"baseUrl": "ghcr.io",
					"subPath": "my-org/components"
				}
			}
		]
	}`)

	cfg, err := resolverspec.Lookup(generic)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Resolvers, 1)

	assert.Equal(t, "ocm.software/core/*", cfg.Resolvers[0].ComponentNamePattern)
	assert.Empty(t, cfg.Resolvers[0].VersionConstraint, "versionConstraint should be empty for old configs")
	assert.NotNil(t, cfg.Resolvers[0].Repository)
}

func TestLookup_WithVersionConstraint(t *testing.T) {
	generic := makeGenericConfig(t, `{
		"type": "resolvers.config.ocm.software/v1alpha1",
		"resolvers": [
			{
				"componentNamePattern": "my-org/*",
				"versionConstraint": ">=1.0.0, <2.0.0",
				"repository": {
					"type": "OCIRepository/v1",
					"baseUrl": "ghcr.io",
					"subPath": "my-org/legacy"
				}
			},
			{
				"componentNamePattern": "my-org/*",
				"versionConstraint": ">=2.0.0",
				"repository": {
					"type": "OCIRepository/v1",
					"baseUrl": "ghcr.io",
					"subPath": "my-org/current"
				}
			}
		]
	}`)

	cfg, err := resolverspec.Lookup(generic)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Resolvers, 2)

	assert.Equal(t, "my-org/*", cfg.Resolvers[0].ComponentNamePattern)
	assert.Equal(t, ">=1.0.0, <2.0.0", cfg.Resolvers[0].VersionConstraint)

	assert.Equal(t, "my-org/*", cfg.Resolvers[1].ComponentNamePattern)
	assert.Equal(t, ">=2.0.0", cfg.Resolvers[1].VersionConstraint)
}

func TestLookup_UnversionedType_BackwardCompatibility(t *testing.T) {
	// Unversioned type (without /v1alpha1 suffix) must still work.
	generic := makeGenericConfig(t, `{
		"type": "resolvers.config.ocm.software",
		"resolvers": [
			{
				"componentNamePattern": "example.com/*",
				"repository": {
					"type": "OCIRepository/v1",
					"baseUrl": "registry.example.com"
				}
			}
		]
	}`)

	cfg, err := resolverspec.Lookup(generic)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Resolvers, 1)

	assert.Equal(t, "example.com/*", cfg.Resolvers[0].ComponentNamePattern)
	assert.Empty(t, cfg.Resolvers[0].VersionConstraint)
}

func TestMerge_PreservesVersionConstraint(t *testing.T) {
	cfg1 := &resolverspec.Config{
		Type: runtime.NewVersionedType(resolverspec.ConfigType, resolverspec.Version),
		Resolvers: []*resolverspec.Resolver{
			{
				Repository:           &runtime.Raw{},
				ComponentNamePattern: "a/*",
				VersionConstraint:    ">=1.0.0",
			},
		},
	}
	cfg2 := &resolverspec.Config{
		Type: runtime.NewVersionedType(resolverspec.ConfigType, resolverspec.Version),
		Resolvers: []*resolverspec.Resolver{
			{
				Repository:           &runtime.Raw{},
				ComponentNamePattern: "b/*",
				VersionConstraint:    ">=2.0.0",
			},
		},
	}

	merged := resolverspec.Merge(cfg1, cfg2)
	require.Len(t, merged.Resolvers, 2)
	assert.Equal(t, ">=1.0.0", merged.Resolvers[0].VersionConstraint)
	assert.Equal(t, ">=2.0.0", merged.Resolvers[1].VersionConstraint)
}
