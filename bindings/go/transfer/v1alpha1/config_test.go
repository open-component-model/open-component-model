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
		{"valid all fields", Config{Recursive: true, CopyMode: CopyModeAllResources, UploadType: UploadAsOciArtifact}, ""},
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

	empty := &Config{}
	r.Equal(CopyModeLocalBlobResources, empty.GetCopyMode())
	r.Equal(UploadAsDefault, empty.GetUploadType())

	populated := &Config{CopyMode: CopyModeAllResources, UploadType: UploadAsOciArtifact}
	r.Equal(CopyModeAllResources, populated.GetCopyMode())
	r.Equal(UploadAsOciArtifact, populated.GetUploadType())
}

func TestConfig_SchemeRoundTrip(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	in := &Config{Recursive: true, CopyMode: CopyModeAllResources, UploadType: UploadAsOciArtifact}
	// DefaultType stamps the canonical wire-format type onto a struct that has
	// none. Asserting the result pins the registration contract from init():
	// the first alias passed to MustRegisterWithAlias (the unversioned form) is
	// what new writes serialise with. A reorder there would flip this assertion.
	_, err := Scheme.DefaultType(in)
	r.NoError(err)
	r.Equal(runtime.NewUnversionedType(ConfigType), in.Type)

	versioned := &Config{Type: runtime.NewVersionedType(ConfigType, Version), Recursive: true, CopyMode: CopyModeAllResources}
	data, err := json.Marshal(versioned)
	r.NoError(err)
	out := &Config{}
	r.NoError(Scheme.Decode(bytes.NewReader(data), out))
	r.Equal(versioned.Recursive, out.Recursive)
	r.Equal(versioned.CopyMode, out.CopyMode)
}

func TestConfig_YAMLRoundTrip(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// Match the production path: CLI's loadTransferConfig parses --transfer-config
	// files via Scheme.Decode (which yaml.Unmarshals under the hood and respects
	// the type discriminator), not via raw yaml.Unmarshal.
	src := []byte("type: TransferConfiguration/v1alpha1\nrecursive: true\ncopyMode: allResources\nuploadType: ociArtifact\n")

	cfg := &Config{}
	r.NoError(Scheme.Decode(bytes.NewReader(src), cfg))
	r.True(cfg.Recursive)
	r.Equal(CopyModeAllResources, cfg.CopyMode)
	r.Equal(UploadAsOciArtifact, cfg.UploadType)

	out, err := yaml.Marshal(cfg)
	r.NoError(err)

	cfg2 := &Config{}
	r.NoError(Scheme.Decode(bytes.NewReader(out), cfg2))
	r.Equal(cfg, cfg2)
}
