package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConfigSchema(t *testing.T) {
	r := require.New(t)
	var document any
	r.NoError(json.Unmarshal(Config{}.JSONSchema(), &document))
	compiler := jsonschema.NewCompiler()
	r.NoError(compiler.AddResource("config.json", document))
	schema, err := compiler.Compile("config.json")
	r.NoError(err)
	for _, tt := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"defaults", `{"type":"GPGSigningConfiguration/v1alpha1"}`, true},
		{"hash and pin", `{"type":"GPGSigningConfiguration/v1alpha1","hashAlgorithm":"SHA-384","keyFingerprint":"0x167C7102F8AC81E4"}`, true},
		{"unversioned alias", `{"type":"GPGSigningConfiguration","hashAlgorithm":"SHA-512"}`, true},
		{"unsupported hash", `{"type":"GPGSigningConfiguration/v1alpha1","hashAlgorithm":"SHA-1"}`, false},
		// keySource was removed; the GnuPG keyring is selected by keyringFingerprint in the credentials.
		{"removed keySource", `{"type":"GPGSigningConfiguration/v1alpha1","keySource":"keyring"}`, false},
		{"missing type", `{"hashAlgorithm":"SHA-256"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var data any
			r.NoError(json.Unmarshal([]byte(tt.data), &data))
			err := schema.Validate(data)
			if tt.valid {
				r.NoError(err)
			} else {
				r.Error(err)
			}
		})
	}
}

// TestScheme_DecodesConfigAliases decodes configs as the signing configuration delivers them: untyped, under the
// versioned type or its unversioned alias.
func TestScheme_DecodesConfigAliases(t *testing.T) {
	for _, typ := range []string{"GPGSigningConfiguration/v1alpha1", "GPGSigningConfiguration"} {
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)
			raw := &runtime.Raw{}
			r.NoError(json.Unmarshal([]byte(`{"type":"`+typ+`","hashAlgorithm":"SHA-384"}`), raw))
			obj, err := Scheme.NewObject(raw.GetType())
			r.NoError(err)
			r.NoError(Scheme.Convert(raw, obj))
			cfg, ok := obj.(*Config)
			r.True(ok, "got %T", obj)
			r.Equal(HashAlgorithmSHA384, cfg.HashAlgorithm)
			r.Equal(typ, cfg.GetType().String())
		})
	}
}
