package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestGPGCredentialsSchema(t *testing.T) {
	r := require.New(t)
	var document any
	r.NoError(json.Unmarshal(GPGCredentials{}.JSONSchema(), &document))
	compiler := jsonschema.NewCompiler()
	r.NoError(compiler.AddResource("credentials.json", document))
	schema, err := compiler.Compile("credentials.json")
	r.NoError(err)
	for _, tt := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"key files", `{"type":"GPGCredentials/v1alpha1","privateKeyPGPFile":"priv.asc","publicKeyPGPFile":"pub.asc","passphrase":"pw"}`, true},
		{"keyring", `{"type":"GPGCredentials/v1alpha1","keyringFingerprint":"0xB118 BE3A","keyringHome":"/gnupg"}`, true},
		{"unversioned alias", `{"type":"GPGCredentials","publicKeyPGP":"pub"}`, true},
		{"missing type", `{"publicKeyPGP":"pub"}`, false},
		{"other type", `{"type":"RSACredentials/v1alpha1"}`, false},
		{"unknown field", `{"type":"GPGCredentials/v1alpha1","keySource":"keyring"}`, false},
		{"non-string fingerprint", `{"type":"GPGCredentials/v1alpha1","keyringFingerprint":42}`, false},
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
