package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	typ := runtime.NewVersionedType(TrustedRootType, Version)

	tests := []struct {
		name       string
		properties map[string]string
		expected   *TrustedRoot
	}{
		{
			name: "all fields populated",
			properties: map[string]string{
				CredentialKeyTrustedRootJSON:     `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json"}`,
				CredentialKeyTrustedRootJSONFile: "/path/to/trusted_root.json",
			},
			expected: &TrustedRoot{
				Type:                typ,
				TrustedRootJSON:     `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json"}`,
				TrustedRootJSONFile: "/path/to/trusted_root.json",
			},
		},
		{
			name:       "empty map",
			properties: map[string]string{},
			expected:   &TrustedRoot{Type: typ},
		},
		{
			name:       "partial fields",
			properties: map[string]string{CredentialKeyTrustedRootJSONFile: "/only/file"},
			expected:   &TrustedRoot{Type: typ, TrustedRootJSONFile: "/only/file"},
		},
		{
			name:       "ignores unknown properties",
			properties: map[string]string{CredentialKeyTrustedRootJSON: "{}", "unknownField": "ignored"},
			expected:   &TrustedRoot{Type: typ, TrustedRootJSON: "{}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FromDirectCredentials(tt.properties))
		})
	}
}

func TestMustRegisterCredentialType(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	obj, err := scheme.NewObject(runtime.NewVersionedType(TrustedRootType, Version))
	r.NoError(err)
	assert.IsType(t, &TrustedRoot{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(TrustedRootType))
	r.NoError(err)
	assert.IsType(t, &TrustedRoot{}, obj)
}

func TestTrustedRoot_TypedJSONParsing(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	raw := &runtime.Raw{}
	raw.Data = []byte(`{"type":"TrustedRoot/v1","trustedRootJSON":"{\"ca\":[]}","trustedRootJSONFile":"/p"}`)
	raw.Type = runtime.NewVersionedType(TrustedRootType, Version)

	creds := &TrustedRoot{}
	r.NoError(scheme.Convert(raw, creds))

	assert.Equal(t, `{"ca":[]}`, creds.TrustedRootJSON)
	assert.Equal(t, "/p", creds.TrustedRootJSONFile)
}

func TestTrustedRoot_SchemeConvert(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &TrustedRoot{
		Type:                runtime.NewVersionedType(TrustedRootType, Version),
		TrustedRootJSON:     `{"mediaType":"test"}`,
		TrustedRootJSONFile: "/trusted/root.json",
	}

	raw := &runtime.Raw{}
	r.NoError(scheme.Convert(original, raw))

	restored := &TrustedRoot{}
	r.NoError(scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.TrustedRootJSON, restored.TrustedRootJSON)
	assert.Equal(t, original.TrustedRootJSONFile, restored.TrustedRootJSONFile)
}
