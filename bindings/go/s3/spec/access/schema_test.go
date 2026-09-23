package access_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

func TestS3_WireSchemas(t *testing.T) {
	for _, version := range []struct {
		name   string
		schema []byte
		types  []string
	}{
		{"v1", v1.S3{}.JSONSchema(), []string{"S3/v1", "s3/v1"}},
		{"v2", v2.S3{}.JSONSchema(), []string{"S3/v2", "s3/v2", "S3", "s3"}},
	} {
		t.Run(version.name, func(t *testing.T) {
			r := require.New(t)
			var document any
			r.NoError(json.Unmarshal(version.schema, &document))
			compiler := jsonschema.NewCompiler()
			r.NoError(compiler.AddResource("s3.json", document))
			schema, err := compiler.Compile("s3.json")
			r.NoError(err)
			for _, typ := range version.types {
				for _, fields := range []struct {
					name, value string
				}{
					{"legacy", `"bucket":"b","key":"k"`},
					{"current", `"bucketName":"b","objectKey":"k"`},
					{"mixed", `"bucket":"b","bucketName":"b","key":"k","objectKey":"k"`},
					{"missing", `"region":"r"`},
				} {
					t.Run(typ+"/"+fields.name, func(t *testing.T) {
						r := require.New(t)
						var data any
						r.NoError(json.Unmarshal([]byte(fmt.Sprintf(`{"type":%q,%s}`, typ, fields.value)), &data))
						err := schema.Validate(data)
						valid := fields.name != "missing" && ((version.name == "v1" && fields.name == "legacy") || typ == "S3" || typ == "s3" || (version.name == "v2" && fields.name == "current"))
						if valid {
							r.NoError(err)
						} else {
							r.Error(err)
						}
					})
				}
			}
		})
	}
}
