package v2_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIsLocalBlob_Nil(t *testing.T) {
	assert.False(t, descriptorv2.IsLocalBlob(nil))
}

func TestIsLocalBlob_LocalBlobStruct(t *testing.T) {
	blob := &descriptorv2.LocalBlob{
		Type: runtime.Type{
			Name:    descriptorv2.LocalBlobAccessType,
			Version: descriptorv2.LocalBlobAccessTypeVersion,
		},
		LocalReference: "sha256:abc123",
		MediaType:      "application/octet-stream",
	}

	assert.True(t, descriptorv2.IsLocalBlob(blob))
}

func TestIsLocalBlob_Raw(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.Type{
			Name:    descriptorv2.LocalBlobAccessType,
			Version: descriptorv2.LocalBlobAccessTypeVersion,
		},
		Data: []byte(`{"type":"localBlob/v1","localReference":"sha256:abc123","mediaType":"application/octet-stream","globalAccess":{"type":"ociArtifact","imageReference":"test/image:1.0"},"referenceName":"test/repo:1.0"}`),
	}

	assert.True(t, descriptorv2.IsLocalBlob(raw))
}

func TestIsLocalBlob_RawOCIArtifact(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.Type{
			Name:    "ociArtifact",
			Version: "v1",
		},
		Data: []byte(`{"type":"ociArtifact/v1","imageReference":"ghcr.io/example/image:v1"}`),
	}

	assert.False(t, descriptorv2.IsLocalBlob(raw))
}

func TestIsLocalBlob_RawUnknownType(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.Type{
			Name:    "unknownAccessType",
			Version: "v1",
		},
		Data: []byte(`{"type":"unknownAccessType/v1","foo":"bar"}`),
	}

	assert.False(t, descriptorv2.IsLocalBlob(raw))
}

func TestIsLocalBlob_RawEmptyType(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.Type{},
		Data: []byte(`{}`),
	}

	assert.False(t, descriptorv2.IsLocalBlob(raw))
}
