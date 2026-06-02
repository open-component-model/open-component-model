package v1alpha1

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

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
	src := []byte("type: TransferConfiguration/v1alpha1\nrecursive: -1\ncopyMode: allResources\nuploadType: ociArtifact\n")

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

// TestConfig_RecursiveTriState pins the wire-format tri-state semantic that the
// replication controller's CRD spec relies on: a YAML document that omits
// "recursive" decodes to a nil pointer (distinguishable from an explicit false),
// and an explicit "recursive: false" decodes to a non-nil pointer holding false.
// This is exactly what the *bool indirection on Config.Recursive buys.
func TestConfig_RecursiveTriState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		yaml      string
		wantValue int // only checked when wantNil is false
	}{
		{"omitted", "type: TransferConfiguration/v1alpha1\n", 0},
		{"explicit true", "type: TransferConfiguration/v1alpha1\nrecursive: -1\n", -1},
		{"explicit false", "type: TransferConfiguration/v1alpha1\nrecursive: 0\n", 0},
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
