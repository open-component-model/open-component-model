package v1alpha1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func decodeGeneric(t *testing.T, yaml string) *genericv1.Config {
	t.Helper()
	var cfg genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &cfg))
	return &cfg
}

func entries(types string, body ...string) string {
	out := "type: generic.config.ocm.software/v1\nconfigurations:\n"
	for _, b := range body {
		out += "  - type: " + types + "\n"
		for _, l := range strings.Split(strings.TrimSpace(b), "\n") {
			if l != "" {
				out += "    " + l + "\n"
			}
		}
	}
	return out
}

const versioned = "caching.oci.config.ocm.software/v1alpha1"

func TestLookupConfig_Decoding(t *testing.T) {
	for _, typ := range []string{versioned, "caching.oci.config.ocm.software"} {
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)
			got, err := v1alpha1.LookupConfig(decodeGeneric(t, entries(typ, "mode: Never\nttl: 1h30m\nmaxBlobSize: 1024")))
			r.NoError(err)
			r.NotNil(got)
			r.Equal(runtime.NewVersionedType(v1alpha1.ConfigType, v1alpha1.Version), got.Type)
			r.Equal(v1alpha1.ModeNever, got.Mode)
			r.Equal(90*time.Minute, time.Duration(*got.TTL))
			r.EqualValues(1024, *got.MaxBlobSize)
		})
	}
}

func TestLookupConfig_Modes(t *testing.T) {
	for _, m := range []v1alpha1.Mode{v1alpha1.ModeAlways, v1alpha1.ModeIfNotPresent, v1alpha1.ModeNever} {
		got, err := v1alpha1.LookupConfig(decodeGeneric(t, entries(versioned, "mode: "+string(m))))
		require.NoError(t, err)
		require.Equal(t, m, got.Mode)
	}
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, entries(versioned, "ttl: 1m")))
	require.NoError(t, err)
	require.Empty(t, got.Mode)
	require.Nil(t, got.MaxBlobSize)
}

func TestLookupConfig_Invalid(t *testing.T) {
	for name, body := range map[string]string{
		"mode":          "mode: Sometimes",
		"malformed ttl": "ttl: soon",
		"numeric ttl":   "ttl: 1000000000",
		"zero ttl":      "ttl: 0s",
		"negative ttl":  "ttl: -5m",
		"zero size":     "maxBlobSize: 0",
		"negative size": "maxBlobSize: -1",
		"string size":   "maxBlobSize: big",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := v1alpha1.LookupConfig(decodeGeneric(t, entries(versioned, body)))
			require.Error(t, err)
		})
	}
}

func TestLookupConfig_Absent(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(nil)
	r.NoError(err)
	r.Nil(got)
	got, err = v1alpha1.LookupConfig(decodeGeneric(t, entries("other.config.ocm.software/v1alpha1", "foo: bar")))
	r.NoError(err)
	r.Nil(got)
}

func TestLookupConfig_Merge(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, entries(versioned,
		"mode: Always\nttl: 1m\nmaxBlobSize: 100",
		"mode: IfNotPresent",
		"ttl: 2m",
	)))
	r.NoError(err)
	r.Equal(v1alpha1.ModeIfNotPresent, got.Mode)
	r.Equal(2*time.Minute, time.Duration(*got.TTL))
	r.EqualValues(100, *got.MaxBlobSize)
	r.Equal(runtime.NewVersionedType(v1alpha1.ConfigType, v1alpha1.Version), got.Type)
}

func TestMergeEmpty(t *testing.T) {
	require.Nil(t, v1alpha1.Merge())
	require.Nil(t, v1alpha1.Merge(nil))
}

func TestDuration_Marshal(t *testing.T) {
	r := require.New(t)
	b, err := json.Marshal(v1alpha1.NewDuration(10 * time.Minute))
	r.NoError(err)
	r.Equal(`"10m0s"`, string(b))
}

func TestValidate_Type(t *testing.T) {
	r := require.New(t)
	r.NoError((&v1alpha1.Config{}).Validate())
	r.NoError((&v1alpha1.Config{Type: runtime.NewUnversionedType(v1alpha1.ConfigType)}).Validate())
	r.Error((&v1alpha1.Config{Type: runtime.NewVersionedType("other", "v1")}).Validate())
}

func TestSchema(t *testing.T) {
	r := require.New(t)
	var schema map[string]any
	r.NoError(json.Unmarshal(v1alpha1.Config{}.JSONSchema(), &schema))
	props := schema["properties"].(map[string]any)
	r.EqualValues(1, props["maxBlobSize"].(map[string]any)["minimum"])
	r.Contains(string(v1alpha1.Config{}.JSONSchema()), `"const": "IfNotPresent"`)

	var dur map[string]any
	r.NoError(json.Unmarshal(v1alpha1.Duration(0).JSONSchema(), &dur))
	r.Equal("string", dur["type"])
	r.NotContains(dur, "anyOf")
}
