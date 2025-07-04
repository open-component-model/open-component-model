package utf8_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/input/utf8"
	v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
)

func TestGetV1UTF8Blob(t *testing.T) {
	tests := []struct {
		name    string
		utf8    v1.UTF8
		wantErr bool
		check   func(t *testing.T, blob blob.ReadOnlyBlob)
	}{
		{
			name: "text input",
			utf8: v1.UTF8{
				Text: "Hello, World!",
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "text/plain", mt)
				require.Implements(t, (*blob.SizeAware)(nil), b)
				size := b.(blob.SizeAware).Size()
				assert.Equal(t, int64(13), size)
				require.Implements(t, (*blob.DigestAware)(nil), b)
				dig, ok := b.(blob.DigestAware).Digest()
				require.True(t, ok)
				assert.Equal(t, digest.FromString("Hello, World!").String(), dig)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, "Hello, World!", string(data))
			},
		},
		{
			name: "text input",
			utf8: v1.UTF8{
				Text:     "Hello, World!",
				Compress: true,
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "text/plain+gzip", mt)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				var buf bytes.Buffer
				writer := gzip.NewWriter(&buf)
				_, err = io.Copy(writer, bytes.NewReader([]byte("Hello, World!")))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				assert.Equal(t, buf.String(), string(data))
			},
		},
		{
			name:    "no content specified",
			utf8:    v1.UTF8{},
			wantErr: true,
		},
		{
			name: "multiple content types specified - text and JSON",
			utf8: v1.UTF8{
				Text: "test",
				JSON: json.RawMessage(`{"test": "value"}`),
			},
			wantErr: true,
		},
		{
			name: "multiple content types specified - JSON and YAML",
			utf8: v1.UTF8{
				JSON: json.RawMessage(`{"test": "value"}`),
				YAML: json.RawMessage(`test: value`),
			},
			wantErr: true,
		},
		{
			name: "multiple content types specified - text and YAML",
			utf8: v1.UTF8{
				Text: "test",
				YAML: json.RawMessage(`test: value`),
			},
			wantErr: true,
		},
		{
			name: "multiple content types specified - all three",
			utf8: v1.UTF8{
				Text: "test",
				JSON: json.RawMessage(`{"test": "value"}`),
				YAML: json.RawMessage(`test: value`),
			},
			wantErr: true,
		},
		{
			name: "invalid JSON input",
			utf8: v1.UTF8{
				JSON: json.RawMessage(`{"invalid": json`),
			},
			wantErr: true,
		},
		{
			name: "invalid formatted JSON input",
			utf8: v1.UTF8{
				FormattedJSON: json.RawMessage(`{"invalid": json`),
			},
			wantErr: true,
		},
		{
			name: "invalid YAML input",
			utf8: v1.UTF8{
				YAML: json.RawMessage(`invalid: yaml: :`),
			},
			wantErr: true,
		},
		{
			name: "valid JSON input",
			utf8: v1.UTF8{
				JSON: json.RawMessage(`{"name": "test", "value": 42, "active": true}`),
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json", mt)
				require.Implements(t, (*blob.SizeAware)(nil), b)
				size := b.(blob.SizeAware).Size()
				assert.Equal(t, int64(40), size) // {"name":"test","value":42,"active":true}
				require.Implements(t, (*blob.DigestAware)(nil), b)
				dig, ok := b.(blob.DigestAware).Digest()
				require.True(t, ok)
				expectedData := `{"name":"test","value":42,"active":true}`
				assert.Equal(t, digest.FromString(expectedData).String(), dig)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, expectedData, string(data))
			},
		},
		{
			name: "valid JSON input with compression",
			utf8: v1.UTF8{
				JSON:     json.RawMessage(`{"name": "test", "value": 42, "active": true}`),
				Compress: true,
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json+gzip", mt)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				var buf bytes.Buffer
				writer := gzip.NewWriter(&buf)
				_, err = io.Copy(writer, bytes.NewReader([]byte(`{"name":"test","value":42,"active":true}`)))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				assert.Equal(t, buf.String(), string(data))
			},
		},
		{
			name: "valid formatted JSON input",
			utf8: v1.UTF8{
				FormattedJSON: json.RawMessage(`{"name": "test", "value": 42, "active": true}`),
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json", mt)
				require.Implements(t, (*blob.SizeAware)(nil), b)
				size := b.(blob.SizeAware).Size()
				assert.Equal(t, int64(53), size) // formatted JSON with 2-space indentation
				require.Implements(t, (*blob.DigestAware)(nil), b)
				dig, ok := b.(blob.DigestAware).Digest()
				require.True(t, ok)
				expectedData := `{
  "name": "test",
  "value": 42,
  "active": true
}`
				assert.Equal(t, digest.FromString(expectedData).String(), dig)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, expectedData, string(data))
			},
		},
		{
			name: "valid formatted JSON input with compression",
			utf8: v1.UTF8{
				FormattedJSON: json.RawMessage(`{"name": "test", "value": 42, "active": true}`),
				Compress:      true,
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json+gzip", mt)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				var buf bytes.Buffer
				writer := gzip.NewWriter(&buf)
				expectedData := `{
  "name": "test",
  "value": 42,
  "active": true
}`
				_, err = io.Copy(writer, bytes.NewReader([]byte(expectedData)))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				assert.Equal(t, buf.String(), string(data))
			},
		},
		{
			name: "valid YAML input",
			utf8: v1.UTF8{
				YAML: json.RawMessage(`{"test": "value"}`),
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/x-yaml", mt)
				require.Implements(t, (*blob.SizeAware)(nil), b)
				size := b.(blob.SizeAware).Size()
				assert.True(t, size > 0)
				require.Implements(t, (*blob.DigestAware)(nil), b)
				dig, ok := b.(blob.DigestAware).Digest()
				require.True(t, ok)
				assert.NotEmpty(t, dig)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				// Verify the YAML content is valid by checking it contains expected keys
				yamlContent := string(data)
				assert.Contains(t, yamlContent, "test: value")
			},
		},
		{
			name: "valid YAML input with compression",
			utf8: v1.UTF8{
				YAML:     json.RawMessage(`{"test": "value"}`),
				Compress: true,
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/x-yaml+gzip", mt)

				b, err := compression.Decompress(b)
				require.NoError(t, err)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, "test: value\n", string(data))
			},
		},
		{
			name: "complex JSON object",
			utf8: v1.UTF8{
				JSON: json.RawMessage(`{
					"string": "hello world",
					"number": 123.45,
					"boolean": true,
					"null": null,
					"array": [1, 2, 3, "four"],
					"object": {
						"nested": "value",
						"deep": {
							"level": 3
						}
					}
				}`),
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json", mt)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)

				// Verify the JSON is valid by parsing it
				var parsed map[string]interface{}
				err = json.Unmarshal(data, &parsed)
				require.NoError(t, err)

				// Check specific values
				assert.Equal(t, "hello world", parsed["string"])
				assert.Equal(t, 123.45, parsed["number"])
				assert.Equal(t, true, parsed["boolean"])
				assert.Nil(t, parsed["null"])

				// Check array
				array, ok := parsed["array"].([]interface{})
				require.True(t, ok)
				assert.Equal(t, 4, len(array))
				assert.Equal(t, float64(1), array[0])
				assert.Equal(t, "four", array[3])

				// Check nested object
				obj, ok := parsed["object"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "value", obj["nested"])

				deep, ok := obj["deep"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, float64(3), deep["level"])
			},
		},
		{
			name: "empty JSON object",
			utf8: v1.UTF8{
				JSON: json.RawMessage(`{}`),
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json", mt)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, "{}", string(data))
			},
		},
		{
			name: "JSON array",
			utf8: v1.UTF8{
				JSON: json.RawMessage(`[1, 2, 3, "four", true, null]`),
			},
			check: func(t *testing.T, b blob.ReadOnlyBlob) {
				require.Implements(t, (*blob.MediaTypeAware)(nil), b)
				mt, ok := b.(blob.MediaTypeAware).MediaType()
				require.True(t, ok)
				assert.Equal(t, "application/json", mt)

				reader, err := b.ReadCloser()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, `[1,2,3,"four",true,null]`, string(data))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob, err := utf8.GetV1UTF8Blob(tt.utf8)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, blob)

			if tt.check != nil {
				tt.check(t, blob)
			}
		})
	}
}
