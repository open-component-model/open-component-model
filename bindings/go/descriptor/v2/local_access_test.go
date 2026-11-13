package v2_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLocalBlob_Struct(t *testing.T) {
	// Setup
	globalAccess := &runtime.Raw{
		Type: runtime.Type{
			Name: "ociArtifact",
		},
		Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
	}

	blob := descriptorv2.LocalBlob{
		Type: runtime.Type{
			Name:    descriptorv2.LocalBlobAccessType,
			Version: descriptorv2.LocalBlobAccessTypeVersion,
		},
		LocalReference: "sha256:abc123",
		MediaType:      "application/octet-stream",
		GlobalAccess:   globalAccess,
		ReferenceName:  "test/repo:1.0",
	}

	// Test
	jsonData, err := json.Marshal(blob)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"type":"LocalBlob/v1"`)
	assert.Contains(t, string(jsonData), `"localReference":"sha256:abc123"`)
	assert.Contains(t, string(jsonData), `"mediaType":"application/octet-stream"`)
	assert.Contains(t, string(jsonData), `"globalAccess":{"type":"ociArtifact","imageReference":"test/image:1.0"}`)
	assert.Contains(t, string(jsonData), `"referenceName":"test/repo:1.0"`)
}

func TestLocalBlob_UnmarshalJSON_LegacyType(t *testing.T) {
	// Setup
	jsonData := `{
		"type": "localBlob/v1",
		"localReference": "sha256:abc123",
		"mediaType": "application/octet-stream",
		"globalAccess": {
			"type": "ociArtifact",
			"imageReference": "test/image:1.0"
		},
		"referenceName": "test/repo:1.0"
	}`

	var blob descriptorv2.LocalBlob
	err := json.Unmarshal([]byte(jsonData), &blob)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, descriptorv2.LegacyLocalBlobAccessType, blob.Type.Name)
	assert.Equal(t, descriptorv2.LocalBlobAccessTypeVersion, blob.Type.Version)
	assert.Equal(t, "sha256:abc123", blob.LocalReference)
	assert.Equal(t, "application/octet-stream", blob.MediaType)
	assert.Equal(t, "test/repo:1.0", blob.ReferenceName)
	require.NotNil(t, blob.GlobalAccess)
	assert.Equal(t, "ociArtifact", blob.GlobalAccess.Name)
}

func TestLocalBlob_UnmarshalJSON_UpperCamelCase(t *testing.T) {
	// Setup
	jsonData := `{
		"type": "LocalBlob/v1",
		"localReference": "sha256:abc123",
		"mediaType": "application/octet-stream",
		"globalAccess": {
			"type": "ociArtifact",
			"imageReference": "test/image:1.0"
		},
		"referenceName": "test/repo:1.0"
	}`

	var blob descriptorv2.LocalBlob
	err := json.Unmarshal([]byte(jsonData), &blob)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "LocalBlob", blob.Type.Name)
	assert.Equal(t, descriptorv2.LocalBlobAccessTypeVersion, blob.Type.Version)
	assert.Equal(t, "sha256:abc123", blob.LocalReference)
	assert.Equal(t, "application/octet-stream", blob.MediaType)
	assert.Equal(t, "test/repo:1.0", blob.ReferenceName)
	require.NotNil(t, blob.GlobalAccess)
	assert.Equal(t, "ociArtifact", blob.GlobalAccess.Name)
}

func TestLocalBlob_UnmarshalJSON_Minimal(t *testing.T) {
	// Setup
	jsonData := `{
		"type": "localBlob/v1",
		"localReference": "sha256:abc123",
		"mediaType": "application/octet-stream"
	}`

	var blob descriptorv2.LocalBlob
	err := json.Unmarshal([]byte(jsonData), &blob)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, descriptorv2.LegacyLocalBlobAccessType, blob.Type.Name)
	assert.Equal(t, descriptorv2.LocalBlobAccessTypeVersion, blob.Type.Version)
	assert.Equal(t, "sha256:abc123", blob.LocalReference)
	assert.Equal(t, "application/octet-stream", blob.MediaType)
	assert.Nil(t, blob.GlobalAccess)
	assert.Empty(t, blob.ReferenceName)
}

func TestLocalBlob_UnmarshalJSON_UnversionedUpperCamelCase(t *testing.T) {
	jsonData := `{
		"type": "LocalBlob",
		"localReference": "sha256:abc123",
		"mediaType": "application/octet-stream"
	}`

	var blob descriptorv2.LocalBlob
	err := json.Unmarshal([]byte(jsonData), &blob)

	require.NoError(t, err)
	assert.Equal(t, descriptorv2.LocalBlobAccessType, blob.Type.Name)
	assert.Empty(t, blob.Type.Version)
	assert.Equal(t, "sha256:abc123", blob.LocalReference)
	assert.Equal(t, "application/octet-stream", blob.MediaType)
}

func TestLocalBlob_UnmarshalJSON_UnversionedLegacy(t *testing.T) {
	jsonData := `{
		"type": "localBlob",
		"localReference": "sha256:abc123",
		"mediaType": "application/octet-stream"
	}`

	var blob descriptorv2.LocalBlob
	err := json.Unmarshal([]byte(jsonData), &blob)

	require.NoError(t, err)
	assert.Equal(t, descriptorv2.LegacyLocalBlobAccessType, blob.Type.Name)
	assert.Empty(t, blob.Type.Version)
	assert.Equal(t, "sha256:abc123", blob.LocalReference)
	assert.Equal(t, "application/octet-stream", blob.MediaType)
}

func TestLocalBlob_Constants(t *testing.T) {
	assert.Equal(t, "LocalBlob", descriptorv2.LocalBlobAccessType)
	assert.Equal(t, "localBlob", descriptorv2.LegacyLocalBlobAccessType)
	assert.Equal(t, "localOciBlob", descriptorv2.LegacyLocalOciBlobAccessType)
	assert.Equal(t, "v1", descriptorv2.LocalBlobAccessTypeVersion)
}

func TestLocalBlob_UnmarshalJSON_LegacyLocalOciBlob(t *testing.T) {
	tests := []struct {
		name               string
		data               string
		wantType           runtime.Type
		wantLocalReference string
	}{
		{
			name:               "versioned legacy type maps digest",
			data:               `{"type":"localOciBlob/v1","digest":"sha256:abc123"}`,
			wantType:           runtime.NewVersionedType(descriptorv2.LegacyLocalOciBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			wantLocalReference: "sha256:abc123",
		},
		{
			name:               "unversioned legacy type maps digest",
			data:               `{"type":"localOciBlob","digest":"sha256:abc123"}`,
			wantType:           runtime.NewUnversionedType(descriptorv2.LegacyLocalOciBlobAccessType),
			wantLocalReference: "sha256:abc123",
		},
		{
			name:               "localReference wins over digest",
			data:               `{"type":"localOciBlob/v1","localReference":"sha256:abc123","digest":"sha256:def456"}`,
			wantType:           runtime.NewVersionedType(descriptorv2.LegacyLocalOciBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			wantLocalReference: "sha256:abc123",
		},
		{
			name:               "digest is ignored for a LocalBlob access",
			data:               `{"type":"LocalBlob/v1","digest":"sha256:abc123"}`,
			wantType:           runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			wantLocalReference: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			var blob descriptorv2.LocalBlob
			r.NoError(json.Unmarshal([]byte(tt.data), &blob))
			r.Equal(tt.wantType, blob.Type)
			r.Equal(tt.wantLocalReference, blob.LocalReference)
			r.Empty(blob.MediaType)
		})
	}
}

// TestLocalBlob_MarshalJSON_DropsLegacyDigest keeps the legacy spelling read-only: a
// descriptor v2 writes never carries the digest field.
func TestLocalBlob_MarshalJSON_DropsLegacyDigest(t *testing.T) {
	r := require.New(t)

	var blob descriptorv2.LocalBlob
	r.NoError(json.Unmarshal([]byte(`{"type":"localOciBlob/v1","digest":"sha256:abc123"}`), &blob))

	data, err := json.Marshal(blob)
	r.NoError(err)
	r.NotContains(string(data), "digest")
	r.Contains(string(data), `"localReference":"sha256:abc123"`)
}
