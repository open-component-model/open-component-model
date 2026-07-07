package v1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

func TestDecodeLegacyOCMAccessSpec(t *testing.T) {
	tests := []struct {
		name    string
		rawType runtime.Type
		json    string
		want    v1.Wget
	}{
		{
			name:    "legacy upper-case URL with versioned type",
			rawType: runtime.NewVersionedType("wget", v1.Version),
			json: `{
				"type": "wget/v1",
				"URL": "https://example.com/file.tar.gz",
				"mediaType": "application/x-tar",
				"header": {"Accept": ["application/octet-stream"]},
				"verb": "GET",
				"noRedirect": true
			}`,
			want: v1.Wget{
				URL:        "https://example.com/file.tar.gz",
				MediaType:  "application/x-tar",
				Header:     map[string][]string{"Accept": {"application/octet-stream"}},
				Verb:       "GET",
				NoRedirect: true,
			},
		},
		{
			name:    "current lower-case url with versioned type",
			rawType: runtime.NewVersionedType("Wget", v1.Version),
			json: `{
				"type": "Wget/v1",
				"url": "https://example.com/file.bin",
				"mediaType": "application/octet-stream"
			}`,
			want: v1.Wget{
				URL:       "https://example.com/file.bin",
				MediaType: "application/octet-stream",
			},
		},
		{
			name:    "legacy upper-case URL with unversioned type",
			rawType: runtime.NewUnversionedType("wget"),
			json:    `{"type": "wget", "URL": "https://example.com/file", "verb": "POST"}`,
			want: v1.Wget{
				URL:  "https://example.com/file",
				Verb: "POST",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var into v1.Wget
			err := access.Scheme.Convert(&runtime.Raw{Type: tt.rawType, Data: []byte(tt.json)}, &into)
			require.NoErrorf(t, err, "failed to decode spec: %s", tt.json)

			assert.Equal(t, tt.want.URL, into.URL)
			assert.Equal(t, tt.want.MediaType, into.MediaType)
			assert.Equal(t, tt.want.Header, into.Header)
			assert.Equal(t, tt.want.Verb, into.Verb)
			assert.Equal(t, tt.want.NoRedirect, into.NoRedirect)
		})
	}
}
