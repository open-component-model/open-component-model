package v2alpha1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		want     string
	}{
		{
			name:     "zero duration",
			duration: Duration(0),
			want:     `"0s"`,
		},
		{
			name:     "one second",
			duration: Duration(time.Second),
			want:     `"1s"`,
		},
		{
			name:     "complex duration",
			duration: Duration(2*time.Hour + 30*time.Minute + 15*time.Second),
			want:     `"2h30m15s"`,
		},
		{
			name:     "sub-millisecond",
			duration: Duration(500 * time.Microsecond),
			want:     `"500µs"`,
		},
		{
			name:     "negative duration",
			duration: Duration(-5 * time.Second),
			want:     `"-5s"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := tt.duration.MarshalJSON()
			r.NoError(err)
			r.JSONEq(tt.want, string(got))
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Duration
		wantErr bool
	}{
		{
			name:  "string: simple seconds",
			input: `"5s"`,
			want:  Duration(5 * time.Second),
		},
		{
			name:  "string: complex duration",
			input: `"1h30m"`,
			want:  Duration(time.Hour + 30*time.Minute),
		},
		{
			name:  "string: zero",
			input: `"0s"`,
			want:  Duration(0),
		},
		{
			name:  "string: negative",
			input: `"-10s"`,
			want:  Duration(-10 * time.Second),
		},
		{
			name:  "numeric: nanoseconds as float64",
			input: `1000000000`,
			want:  Duration(time.Second),
		},
		{
			name:  "numeric: zero",
			input: `0`,
			want:  Duration(0),
		},
		{
			name:  "numeric: fractional nanoseconds",
			input: `1500000000`,
			want:  Duration(1500 * time.Millisecond),
		},
		{
			name:    "invalid: not a valid duration string",
			input:   `"notaduration"`,
			wantErr: true,
		},
		{
			name:    "invalid: boolean",
			input:   `true`,
			wantErr: true,
		},
		{
			name:    "invalid: null json",
			input:   `null`,
			wantErr: true,
		},
		{
			name:    "invalid: array",
			input:   `[1,2]`,
			wantErr: true,
		},
		{
			name:    "invalid: object",
			input:   `{"seconds":5}`,
			wantErr: true,
		},
		{
			name:    "invalid: malformed json",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var d Duration
			err := d.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, d)
		})
	}
}

func TestDuration_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
	}{
		{name: "zero", duration: Duration(0)},
		{name: "one second", duration: Duration(time.Second)},
		{name: "complex", duration: Duration(3*time.Hour + 15*time.Minute + 42*time.Second)},
		{name: "negative", duration: Duration(-30 * time.Second)},
		{name: "sub-second", duration: Duration(250 * time.Millisecond)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			data, err := tt.duration.MarshalJSON()
			r.NoError(err)

			var got Duration
			err = got.UnmarshalJSON(data)
			r.NoError(err)
			r.Equal(tt.duration, got)
		})
	}
}

func TestDuration_JSONStructIntegration(t *testing.T) {
	r := require.New(t)
	type wrapper struct {
		Timeout Duration `json:"timeout"`
	}

	w := wrapper{Timeout: Duration(5 * time.Second)}
	data, err := json.Marshal(w)
	r.NoError(err)
	r.Contains(string(data), `"5s"`)

	var got wrapper
	err = json.Unmarshal(data, &got)
	r.NoError(err)
	r.Equal(w.Timeout, got.Timeout)
}

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
	r.Equal(Duration(0), cfg.IdleTimeout)
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
	r.Equal(Duration(0), cfg.IdleTimeout)
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
	r.Equal(Duration(0), cfg.IdleTimeout)
	r.Empty(cfg.Locations)
}

func TestLookupConfig_SinglePluginConfig(t *testing.T) {
	r := require.New(t)
	pluginCfg := &Config{
		Type:        runtime.NewVersionedType(ConfigType, Version),
		IdleTimeout: Duration(10 * time.Second),
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
	r.Equal(Duration(10*time.Second), cfg.IdleTimeout)
	r.Equal([]string{"/usr/local/plugins"}, cfg.Locations)
}

func TestLookupConfig_MultipleConfigs_Merged(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{
		Type:        runtime.NewVersionedType(ConfigType, Version),
		IdleTimeout: Duration(5 * time.Second),
		Locations:   []string{"/path/a"},
	}
	cfg2 := &Config{
		Type:        runtime.NewVersionedType(ConfigType, Version),
		IdleTimeout: Duration(30 * time.Second),
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
	r.Equal(Duration(30*time.Second), cfg.IdleTimeout)
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
		IdleTimeout: Duration(10 * time.Second),
		Locations:   []string{"/a"},
	}
	result := Merge(cfg)
	r.NotNil(result)
	r.Equal(Duration(10*time.Second), result.IdleTimeout)
	r.Equal([]string{"/a"}, result.Locations)
}

func TestMerge_IdleTimeout_TakesMax(t *testing.T) {
	tests := []struct {
		name     string
		timeouts []Duration
		want     Duration
	}{
		{
			name:     "ascending order",
			timeouts: []Duration{Duration(1 * time.Second), Duration(5 * time.Second), Duration(10 * time.Second)},
			want:     Duration(10 * time.Second),
		},
		{
			name:     "descending order",
			timeouts: []Duration{Duration(10 * time.Second), Duration(5 * time.Second), Duration(1 * time.Second)},
			want:     Duration(10 * time.Second),
		},
		{
			name:     "all equal",
			timeouts: []Duration{Duration(3 * time.Second), Duration(3 * time.Second)},
			want:     Duration(3 * time.Second),
		},
		{
			name:     "all zero",
			timeouts: []Duration{Duration(0), Duration(0)},
			want:     Duration(0),
		},
		{
			name:     "middle is max",
			timeouts: []Duration{Duration(2 * time.Second), Duration(10 * time.Second), Duration(3 * time.Second)},
			want:     Duration(10 * time.Second),
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
	cfg := &Config{IdleTimeout: Duration(time.Second)}
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
	cfg1 := &Config{IdleTimeout: Duration(-5 * time.Second)}
	cfg2 := &Config{IdleTimeout: Duration(0)}

	result := Merge(cfg1, cfg2)
	r.NotNil(result)
	r.Equal(Duration(0), result.IdleTimeout)
}

func TestMerge_DoesNotMutateInputs(t *testing.T) {
	r := require.New(t)
	cfg1 := &Config{
		IdleTimeout: Duration(time.Second),
		Locations:   []string{"/a"},
	}
	cfg2 := &Config{
		IdleTimeout: Duration(2 * time.Second),
		Locations:   []string{"/b"},
	}

	// Save copies to verify no mutation.
	origLocs1 := append([]string{}, cfg1.Locations...)
	origLocs2 := append([]string{}, cfg2.Locations...)

	_ = Merge(cfg1, cfg2)

	r.Equal(origLocs1, cfg1.Locations)
	r.Equal(origLocs2, cfg2.Locations)
}
