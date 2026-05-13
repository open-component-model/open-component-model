package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
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

func TestLookup(t *testing.T) {
	t.Run("snippet round trips", func(t *testing.T) {
		generic := makeGenericConfig(t, `{
			"type": "ownership.config.ocm.software/v1alpha1",
			"policy": "auto",
			"repositories": [
				{
					"repository": { "type": "OCIRepository/v1" },
					"policy": "disabled"
				},
				{
					"repository": {
						"type": "OCIRepository/v1",
						"baseUrl": "ghcr.io",
						"subPath": "my-org/components"
					},
					"policy": "auto"
				}
			]
		}`)

		cfg, err := ownershipv1alpha1.Lookup(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, ownershipv1alpha1.PolicyAuto, cfg.Policy)
		require.Len(t, cfg.Repositories, 2)

		assert.Equal(t, ownershipv1alpha1.PolicyDisabled, cfg.Repositories[0].Policy)
		require.NotNil(t, cfg.Repositories[0].Repository)
		assert.Equal(t, "OCIRepository/v1", cfg.Repositories[0].Repository.Type.String())

		assert.Equal(t, ownershipv1alpha1.PolicyAuto, cfg.Repositories[1].Policy)
		require.NotNil(t, cfg.Repositories[1].Repository)
		assert.Equal(t, "OCIRepository/v1", cfg.Repositories[1].Repository.Type.String())
	})

	t.Run("no config defaults to disabled", func(t *testing.T) {
		generic := makeGenericConfig(t, `{
			"type": "ownership.config.ocm.software/v1alpha1"
		}`)

		cfg, err := ownershipv1alpha1.Lookup(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Empty(t, cfg.Policy, "policy must default to empty (disabled) when omitted")
		assert.Empty(t, cfg.Repositories)
	})

	t.Run("unversioned type backward compatibility", func(t *testing.T) {
		generic := makeGenericConfig(t, `{
			"type": "ownership.config.ocm.software",
			"policy": "auto"
		}`)

		cfg, err := ownershipv1alpha1.Lookup(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, ownershipv1alpha1.PolicyAuto, cfg.Policy)
	})
}

func TestMerge(t *testing.T) {
	t.Run("last policy wins and repositories concatenated", func(t *testing.T) {
		cfg1 := &ownershipv1alpha1.Config{
			Type:   runtime.NewVersionedType(ownershipv1alpha1.ConfigType, ownershipv1alpha1.Version),
			Policy: ownershipv1alpha1.PolicyDisabled,
			Repositories: []*ownershipv1alpha1.RepositoryPolicy{
				{Repository: &runtime.Raw{}, Policy: ownershipv1alpha1.PolicyDisabled},
			},
		}
		cfg2 := &ownershipv1alpha1.Config{
			Type:   runtime.NewVersionedType(ownershipv1alpha1.ConfigType, ownershipv1alpha1.Version),
			Policy: ownershipv1alpha1.PolicyAuto,
			Repositories: []*ownershipv1alpha1.RepositoryPolicy{
				{Repository: &runtime.Raw{}, Policy: ownershipv1alpha1.PolicyAuto},
			},
		}

		merged := ownershipv1alpha1.Merge(cfg1, cfg2)
		require.NotNil(t, merged)
		assert.Equal(t, ownershipv1alpha1.PolicyAuto, merged.Policy)
		require.Len(t, merged.Repositories, 2)
		assert.Equal(t, ownershipv1alpha1.PolicyDisabled, merged.Repositories[0].Policy)
		assert.Equal(t, ownershipv1alpha1.PolicyAuto, merged.Repositories[1].Policy)
	})

	t.Run("empty policy does not override", func(t *testing.T) {
		cfg1 := &ownershipv1alpha1.Config{Policy: ownershipv1alpha1.PolicyAuto}
		cfg2 := &ownershipv1alpha1.Config{}

		merged := ownershipv1alpha1.Merge(cfg1, cfg2)
		require.NotNil(t, merged)
		assert.Equal(t, ownershipv1alpha1.PolicyAuto, merged.Policy)
	})
}
