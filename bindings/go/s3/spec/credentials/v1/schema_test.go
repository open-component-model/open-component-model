package v1

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestS3CredentialsSchema(t *testing.T) {
	r := require.New(t)
	var document any
	r.NoError(json.Unmarshal(S3Credentials{}.JSONSchema(), &document))
	compiler := jsonschema.NewCompiler()
	r.NoError(compiler.AddResource("credentials.json", document))
	schema, err := compiler.Compile("credentials.json")
	r.NoError(err)
	for _, tt := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"omitted", `{"type":"S3Credentials/v1"}`, true},
		{"false", `{"type":"S3Credentials/v1","anonymous":false}`, true},
		{"true", `{"type":"S3Credentials/v1","anonymous":true}`, true},
		{"alias", `{"type":"S3Credentials","anonymous":true}`, true},
		{"string", `{"type":"S3Credentials/v1","anonymous":"true"}`, false},
		{"number", `{"type":"S3Credentials/v1","anonymous":1}`, false},
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
