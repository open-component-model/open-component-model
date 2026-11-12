package v2alpha1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	typesv1 "ocm.software/open-component-model/bindings/go/configuration/types/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// makeRaw creates a runtime.Raw from a Config by JSON marshalling.
func makeRaw(t *testing.T, cfg *Config) *runtime.Raw {
	t.Helper()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(data, raw))
	return raw
}

func TestLookupConfig_NilConfig(t *testing.T) {
	r := require.New(t)
	cfg, err := LookupConfig(nil)
	r.NoError(err)
	r.NotNil(cfg)
	r.Equal(typesv1.Duration(0), cfg.IdleTimeout)
	r.Empty(cfg.Locations)
}

func TestLookupConfig_EmptyConfig(t *testing.T) {
	r := require.New(t)
	genericCfg := &generic.Config{
		Type:           runtime.NewVersionedType("generic.config.ocm.software", "v1"),
		Configurations: nil,
	}
	cfg, err := LookupConfig(genericCfg)
	r.NoError(err)
	r.NotNil(cfg)
	r.Equal(typesv1.Duration(0), cfg.IdleTimeout)
	r.Empty(cfg.Locations)
}

func TestLookupConfig_NoMatchingPluginConfig(t *testing.T) {
	r := require.New(t)
	// A generic config with entries of a different type.
	otherRaw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(
		[]byte(`{"type":"other.type/v1","value":"x"}`), otherRaw))

	genericCfg := &generic.Config{
		Type:           runtime.NewVersionedType("generic.config.ocm.software", "v1"),
		Configurations: []*runtime.Raw{otherRaw},
	}
	cfg, err := LookupConfig(genericCfg)
	r.NoError(err)
	r.NotNil(cfg)
	r.Equal(typesv1.Duration(0), cfg.IdleTimeout)
	r.Empty(cfg.Locations)
}

func TestLookupConfig_SinglePluginConfig(t *testing.T) {
	r := require.New(t)
	pluginCfg := &Config{
		Type:        runtime.NewVersionedType(ConfigType, Version),
		IdleTimeout: typesv1.Duration(10 * time.Second),
		Locations:   []string{"/usr/local/plugins"},
	}
	raw := makeRaw(t, pluginCfg)

	genericCfg := &generic.Config{
		Type:           runtime.NewVersionedType("generic.config.ocm.software", "v1"),
		Configurations: []*runtime.Raw{raw},
	}

	cfg, err := LookupConfig(genericCfg)
	r.NoError(err)
	r.NotNil(cfg)
	r.Equal(typesv1.Duration(10*time.Second), cfg.IdleTimeout)
	r.Equal([]string{"/usr/local/plugins"}, cfg.Locations)
}

func TestLookupConfig_MultipleConfigs_Merged(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{
		Type:        runtime.NewVersionedType(ConfigType, Version),
		IdleTimeout: typesv1.Duration(5 * time.Second),
		Locations:   []string{"/path/a"},
	}
	cfg2 := &Config{
		Type:        runtime.NewVersionedType(ConfigType, Version),
		IdleTimeout: typesv1.Duration(30 * time.Second),
		Locations:   []string{"/path/b", "/path/c"},
	}

	genericCfg := &generic.Config{
		Type: runtime.NewVersionedType("generic.config.ocm.software", "v1"),
		Configurations: []*runtime.Raw{
			makeRaw(t, cfg1),
			makeRaw(t, cfg2),
		},
	}

	cfg, err := LookupConfig(genericCfg)
	r.NoError(err)
	r.NotNil(cfg)
	// IdleTimeout is the max.
	r.Equal(typesv1.Duration(30*time.Second), cfg.IdleTimeout)
	// Locations are appended in order.
	r.Equal([]string{"/path/a", "/path/b", "/path/c"}, cfg.Locations)
}

func TestLookupConfig_InvalidEntryDecoding(t *testing.T) {
	r := require.New(t)
	// Craft a Raw that has the right type but bogus content that cannot be
	// unmarshalled into a Config (e.g. idleTimeout is an object instead of
	// a duration string or number).
	badRaw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(
		`{"type":"plugin.config.ocm.software/v2alpha1","idleTimeout":{"bad":true},"locations":[]}`),
		badRaw,
	))

	genericCfg := &generic.Config{
		Type:           runtime.NewVersionedType("generic.config.ocm.software", "v1"),
		Configurations: []*runtime.Raw{badRaw},
	}

	_, err := LookupConfig(genericCfg)
	r.Error(err)
	r.Contains(err.Error(), "failed to decode credential config")
}

func TestMerge_Empty(t *testing.T) {
	r := require.New(t)
	result := Merge()
	r.Nil(result)
}

func TestMerge_SingleConfig(t *testing.T) {
	r := require.New(t)
	cfg := &Config{
		IdleTimeout: typesv1.Duration(10 * time.Second),
		Locations:   []string{"/a"},
	}
	result := Merge(cfg)
	r.NotNil(result)
	r.Equal(typesv1.Duration(10*time.Second), result.IdleTimeout)
	r.Equal([]string{"/a"}, result.Locations)
}

func TestMerge_IdleTimeout_TakesMax(t *testing.T) {
	tests := []struct {
		name     string
		timeouts []typesv1.Duration
		want     typesv1.Duration
	}{
		{
			name:     "ascending order",
			timeouts: []typesv1.Duration{typesv1.Duration(1 * time.Second), typesv1.Duration(5 * time.Second), typesv1.Duration(10 * time.Second)},
			want:     typesv1.Duration(10 * time.Second),
		},
		{
			name:     "descending order",
			timeouts: []typesv1.Duration{typesv1.Duration(10 * time.Second), typesv1.Duration(5 * time.Second), typesv1.Duration(1 * time.Second)},
			want:     typesv1.Duration(10 * time.Second),
		},
		{
			name:     "all equal",
			timeouts: []typesv1.Duration{typesv1.Duration(3 * time.Second), typesv1.Duration(3 * time.Second)},
			want:     typesv1.Duration(3 * time.Second),
		},
		{
			name:     "all zero",
			timeouts: []typesv1.Duration{typesv1.Duration(0), typesv1.Duration(0)},
			want:     typesv1.Duration(0),
		},
		{
			name:     "middle is max",
			timeouts: []typesv1.Duration{typesv1.Duration(2 * time.Second), typesv1.Duration(10 * time.Second), typesv1.Duration(3 * time.Second)},
			want:     typesv1.Duration(10 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			configs := make([]*Config, len(tt.timeouts))
			for i, d := range tt.timeouts {
				configs[i] = &Config{IdleTimeout: d}
			}
			result := Merge(configs...)
			r.NotNil(result)
			r.Equal(tt.want, result.IdleTimeout)
		})
	}
}

func TestMerge_Locations_Appended(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{Locations: []string{"/a", "/b"}}
	cfg2 := &Config{Locations: []string{"/c"}}
	cfg3 := &Config{Locations: []string{"/d", "/e"}}

	result := Merge(cfg1, cfg2, cfg3)
	r.NotNil(result)
	r.Equal([]string{"/a", "/b", "/c", "/d", "/e"}, result.Locations)
}

func TestMerge_Locations_NilLocations(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{Locations: nil}
	cfg2 := &Config{Locations: []string{"/x"}}

	result := Merge(cfg1, cfg2)
	r.NotNil(result)
	r.Equal([]string{"/x"}, result.Locations)
}

func TestMerge_Locations_AllNil(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{Locations: nil}
	cfg2 := &Config{Locations: nil}

	result := Merge(cfg1, cfg2)
	r.NotNil(result)
	r.Empty(result.Locations)
}

func TestMerge_SetsDefaultType(t *testing.T) {
	r := require.New(t)
	// Merge calls scheme.DefaultType which should set the registered type.
	cfg := &Config{IdleTimeout: typesv1.Duration(time.Second)}
	result := Merge(cfg)
	r.NotNil(result)
	// The scheme registers Config with ConfigType/Version, so DefaultType
	// should set that on the new empty Config.
	r.Equal(ConfigType, result.Type.Name)
	r.Equal(Version, result.Type.Version)
}

func TestMerge_NegativeTimeout_NotPromoted(t *testing.T) {
	// Merge uses > comparison. A negative timeout should not override zero.
	r := require.New(t)
	cfg1 := &Config{IdleTimeout: typesv1.Duration(-5 * time.Second)}
	cfg2 := &Config{IdleTimeout: typesv1.Duration(0)}

	result := Merge(cfg1, cfg2)
	r.NotNil(result)
	r.Equal(typesv1.Duration(0), result.IdleTimeout)
}

func TestMerge_DoesNotMutateInputs(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{
		IdleTimeout: typesv1.Duration(time.Second),
		Locations:   []string{"/a"},
	}
	cfg2 := &Config{
		IdleTimeout: typesv1.Duration(2 * time.Second),
		Locations:   []string{"/b"},
	}

	// Save copies to verify no mutation.
	origLocs1 := append([]string{}, cfg1.Locations...)
	origLocs2 := append([]string{}, cfg2.Locations...)

	_ = Merge(cfg1, cfg2)

	r.Equal(origLocs1, cfg1.Locations)
	r.Equal(origLocs2, cfg2.Locations)
}
