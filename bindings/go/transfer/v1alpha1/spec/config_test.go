package spec

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"valid empty", Config{}, ""},
		{"valid all fields", Config{Recursive: -1, CopyMode: CopyModeAllResources, UploadType: UploadAsOciArtifact}, ""},
		{"valid recursive false", Config{Recursive: 0}, ""},
		{"valid copyMode localBlob", Config{CopyMode: CopyModeLocalBlobResources}, ""},
		{"valid uploadType default", Config{UploadType: UploadAsDefault}, ""},
		{"valid uploadType localBlob", Config{UploadType: UploadAsLocalBlob}, ""},
		{"invalid copyMode", Config{CopyMode: "garbage"}, "invalid copyMode"},
		{"invalid uploadType", Config{UploadType: "garbage"}, "invalid uploadType"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, tc.wantErr)
			}
		})
	}
}

func TestConfig_GetDefaults(t *testing.T) {
	t.Parallel()

	r := require.New(t)
	var nilCfg *Config
	r.Equal(CopyModeLocalBlobResources, nilCfg.GetCopyMode())
	r.Equal(UploadAsDefault, nilCfg.GetUploadType())
	r.Equal(0, nilCfg.GetRecursive())

	empty := &Config{}
	r.Equal(CopyModeLocalBlobResources, empty.GetCopyMode())
	r.Equal(UploadAsDefault, empty.GetUploadType())
	r.Equal(0, nilCfg.GetRecursive())

	populated := &Config{Recursive: -1, CopyMode: CopyModeAllResources, UploadType: UploadAsOciArtifact}
	r.Equal(CopyModeAllResources, populated.GetCopyMode())
	r.Equal(UploadAsOciArtifact, populated.GetUploadType())
	r.Equal(-1, populated.GetRecursive())
}

func TestConfig_SchemeRoundTrip(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	in := &Config{Recursive: -1, CopyMode: CopyModeAllResources, UploadType: UploadAsOciArtifact}
	// DefaultType stamps the canonical wire-format type onto a struct that has
	// none. Asserting the result pins the registration contract from init():
	// the first alias passed to MustRegisterWithAlias (the versioned form) is
	// what new writes serialise with. A reorder there would flip this assertion.
	_, err := Scheme.DefaultType(in)
	r.NoError(err)
	r.Equal(runtime.NewVersionedType(ConfigType, Version), in.Type)

	versioned := &Config{Type: runtime.NewVersionedType(ConfigType, Version), Recursive: -1, CopyMode: CopyModeAllResources}
	data, err := json.Marshal(versioned)
	r.NoError(err)
	out := &Config{}
	r.NoError(Scheme.Decode(bytes.NewReader(data), out))
	r.Equal(versioned.GetRecursive(), out.GetRecursive())
	r.Equal(versioned.CopyMode, out.CopyMode)
}

func TestConfig_YAMLRoundTrip(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// Match the production path: CLI's loadTransferConfig parses --transfer-config
	// files via Scheme.Decode (which yaml.Unmarshals under the hood and respects
	// the type discriminator), not via raw yaml.Unmarshal.
	src := []byte("type: transfer.config.ocm.software/v1alpha1\nrecursive: -1\ncopyMode: allResources\nuploadType: ociArtifact\n")

	cfg := &Config{}
	r.NoError(Scheme.Decode(bytes.NewReader(src), cfg))
	r.Equal(-1, cfg.GetRecursive())
	r.Equal(CopyModeAllResources, cfg.CopyMode)
	r.Equal(UploadAsOciArtifact, cfg.UploadType)

	out, err := yaml.Marshal(cfg)
	r.NoError(err)

	cfg2 := &Config{}
	r.NoError(Scheme.Decode(bytes.NewReader(out), cfg2))
	r.Equal(cfg, cfg2)
}

// TestConfig_RecursiveIntOrBool pins the wire-format flexibility of
// [Config.Recursive]: it decodes from either an integer depth or a boolean
// shorthand (true maps to infinite recursion -1, false to no recursion 0), and
// an omitted field decodes to the zero depth.
func TestConfig_RecursiveIntOrBool(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		yaml      string
		wantValue Recursive
	}{
		{"omitted", "type: transfer.config.ocm.software/v1alpha1\n", RecursiveNone},
		{"explicit infinite", "type: transfer.config.ocm.software/v1alpha1\nrecursive: -1\n", RecursiveInfinite},
		{"explicit none", "type: transfer.config.ocm.software/v1alpha1\nrecursive: 0\n", RecursiveNone},
		{"explicit depth", "type: transfer.config.ocm.software/v1alpha1\nrecursive: 3\n", 3},
		{"bool true", "type: transfer.config.ocm.software/v1alpha1\nrecursive: true\n", RecursiveInfinite},
		{"bool false", "type: transfer.config.ocm.software/v1alpha1\nrecursive: false\n", RecursiveNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			cfg := &Config{}
			r.NoError(Scheme.Decode(bytes.NewReader([]byte(tc.yaml)), cfg))
			r.Equal(tc.wantValue, cfg.Recursive)
		})
	}
}

// TestRecursive_MarshalIsAlwaysInt confirms a Recursive set from a boolean
// round-trips back out as its integer form, not as a boolean.
func TestRecursive_MarshalIsAlwaysInt(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var rec Recursive
	r.NoError(rec.UnmarshalJSON([]byte("true")))
	r.Equal(RecursiveInfinite, rec)

	out, err := rec.MarshalJSON()
	r.NoError(err)
	r.Equal("-1", string(out))
}

func TestRecursive_UnmarshalRejectsGarbage(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var rec Recursive
	r.ErrorContains(rec.UnmarshalJSON([]byte(`"nope"`)), "must be a boolean or an integer")
	r.ErrorContains(rec.UnmarshalJSON([]byte("1.5")), "must be a whole number")
}

func makeGenericConfig(t *testing.T, entries ...string) *genericv1.Config {
	t.Helper()
	cfg := &genericv1.Config{
		Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
	}
	for _, entry := range entries {
		raw := &runtime.Raw{}
		require.NoError(t, json.Unmarshal([]byte(entry), raw))
		cfg.Configurations = append(cfg.Configurations, raw)
	}
	return cfg
}

func TestLookupConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil central config", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		cfg, err := LookupConfig(nil)
		r.NoError(err)
		r.Nil(cfg)
	})

	t.Run("no transfer entries", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		generic := makeGenericConfig(t, `{"type": "other.config.ocm.software/v1"}`)
		cfg, err := LookupConfig(generic)
		r.NoError(err)
		r.Nil(cfg)
	})

	t.Run("single entry", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		generic := makeGenericConfig(t,
			`{"type": "transfer.config.ocm.software/v1alpha1", "recursive": -1, "copyMode": "allResources"}`,
		)
		cfg, err := LookupConfig(generic)
		r.NoError(err)
		r.NotNil(cfg)
		r.Equal(-1, cfg.GetRecursive())
		r.Equal(CopyModeAllResources, cfg.CopyMode)
		r.Equal(UploadAsDefault, cfg.GetUploadType())
	})

	t.Run("unversioned type alias", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		generic := makeGenericConfig(t,
			`{"type": "transfer.config.ocm.software", "copyMode": "allResources"}`,
		)
		cfg, err := LookupConfig(generic)
		r.NoError(err)
		r.NotNil(cfg)
		r.Equal(CopyModeAllResources, cfg.CopyMode)
	})

	t.Run("later entry wins, unset fields fall through", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		generic := makeGenericConfig(t,
			`{"type": "transfer.config.ocm.software/v1alpha1", "recursive": 3, "copyMode": "localBlob", "uploadType": "localBlob"}`,
			`{"type": "transfer.config.ocm.software/v1alpha1", "copyMode": "allResources"}`,
		)
		cfg, err := LookupConfig(generic)
		r.NoError(err)
		r.NotNil(cfg)
		r.Equal(3, cfg.GetRecursive())
		r.Equal(CopyModeAllResources, cfg.CopyMode)
		r.Equal(UploadAsLocalBlob, cfg.UploadType)
	})

	t.Run("invalid entry is rejected", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		generic := makeGenericConfig(t,
			`{"type": "transfer.config.ocm.software/v1alpha1", "copyMode": "garbage"}`,
		)
		_, err := LookupConfig(generic)
		r.ErrorContains(err, "invalid copyMode")
	})
}
