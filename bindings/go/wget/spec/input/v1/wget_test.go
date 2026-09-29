package v1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

func TestDecodeInputSpec(t *testing.T) {
	tests := []struct {
		name    string
		rawType runtime.Type
		json    string
		want    v1.Wget
	}{
		{
			name:    "lower-case wget with versioned type",
			rawType: runtime.NewVersionedType("wget", v1.Version),
			json:    `{"type": "wget/v1", "url": "https://example.com/file.tar.gz", "mediaType": "application/x-tar"}`,
			want: v1.Wget{
				URL:       "https://example.com/file.tar.gz",
				MediaType: "application/x-tar",
			},
		},
		{
			name:    "Wget with unversioned type",
			rawType: runtime.NewUnversionedType("Wget"),
			json:    `{"type": "Wget", "url": "https://example.com/file", "verb": "POST"}`,
			want: v1.Wget{
				URL:  "https://example.com/file",
				Verb: "POST",
			},
		},
		{
			name:    "HTTP alias with versioned type",
			rawType: runtime.NewVersionedType("HTTP", v1.Version),
			json:    `{"type": "HTTP/v1", "url": "https://example.com/file.tar.gz", "mediaType": "application/x-tar"}`,
			want: v1.Wget{
				URL:       "https://example.com/file.tar.gz",
				MediaType: "application/x-tar",
			},
		},
		{
			name:    "lowercase http alias with versioned type",
			rawType: runtime.NewVersionedType("http", v1.Version),
			json:    `{"type": "http/v1", "url": "https://example.com/file.bin", "verb": "PUT"}`,
			want: v1.Wget{
				URL:  "https://example.com/file.bin",
				Verb: "PUT",
			},
		},
		{
			name:    "HTTP alias with unversioned type",
			rawType: runtime.NewUnversionedType("HTTP"),
			json:    `{"type": "HTTP", "url": "https://example.com/file", "noRedirect": true}`,
			want: v1.Wget{
				URL:        "https://example.com/file",
				NoRedirect: true,
			},
		},
		{
			name:    "lowercase http alias with unversioned type",
			rawType: runtime.NewUnversionedType("http"),
			json:    `{"type": "http", "url": "https://example.com/file"}`,
			want: v1.Wget{
				URL: "https://example.com/file",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var into v1.Wget
			err := input.Scheme.Convert(&runtime.Raw{Type: tt.rawType, Data: []byte(tt.json)}, &into)
			require.NoErrorf(t, err, "failed to decode spec: %s", tt.json)

			assert.Equal(t, tt.want.URL, into.URL)
			assert.Equal(t, tt.want.MediaType, into.MediaType)
			assert.Equal(t, tt.want.Verb, into.Verb)
			assert.Equal(t, tt.want.NoRedirect, into.NoRedirect)
		})
	}
}

func TestWget_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   v1.Wget
		wantErr string
	}{
		{
			name:  "valid",
			input: v1.Wget{URL: "https://example.com/artifact.tar.gz"},
		},
		{
			name:    "missing url",
			input:   v1.Wget{},
			wantErr: "url is required",
		},
		{
			name:    "unsupported scheme",
			input:   v1.Wget{URL: "ftp://example.com/artifact.tar.gz"},
			wantErr: "url must use the http or https scheme",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
