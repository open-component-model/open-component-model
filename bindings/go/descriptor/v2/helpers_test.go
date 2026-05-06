package v2_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIsLocalBlob(t *testing.T) {
	cases := []struct {
		name  string
		input runtime.Typed
		want  bool
	}{
		{"nil", nil, false},
		{
			"LocalBlob struct",
			&descriptorv2.LocalBlob{
				Type: runtime.Type{
					Name:    descriptorv2.LocalBlobAccessType,
					Version: descriptorv2.LocalBlobAccessTypeVersion,
				},
				LocalReference: "sha256:abc123",
				MediaType:      "application/octet-stream",
			},
			true,
		},
		{
			"raw LocalBlob",
			&runtime.Raw{
				Type: runtime.Type{
					Name:    descriptorv2.LocalBlobAccessType,
					Version: descriptorv2.LocalBlobAccessTypeVersion,
				},
				Data: []byte(`{"type":"LocalBlob/v1","localReference":"sha256:abc123","mediaType":"application/octet-stream","globalAccess":{"type":"ociArtifact","imageReference":"test/image:1.0"},"referenceName":"test/repo:1.0"}`),
			},
			true,
		},
		{
			"raw ociArtifact",
			&runtime.Raw{
				Type: runtime.Type{
					Name:    "ociArtifact",
					Version: "v1",
				},
				Data: []byte(`{"type":"ociArtifact/v1","imageReference":"ghcr.io/example/image:v1"}`),
			},
			false,
		},
		{
			"raw legacy localBlob",
			&runtime.Raw{
				Type: runtime.Type{
					Name:    descriptorv2.LegacyLocalBlobAccessType,
					Version: descriptorv2.LocalBlobAccessTypeVersion,
				},
				Data: []byte(`{"type":"localBlob/v1","localReference":"sha256:abc123","mediaType":"application/octet-stream"}`),
			},
			true,
		},
		{
			"raw unknown type",
			&runtime.Raw{
				Type: runtime.Type{
					Name:    "unknownAccessType",
					Version: "v1",
				},
				Data: []byte(`{"type":"unknownAccessType/v1","foo":"bar"}`),
			},
			false,
		},
		{
			"raw empty type",
			&runtime.Raw{
				Type: runtime.Type{},
				Data: []byte(`{}`),
			},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, descriptorv2.IsLocalBlob(tc.input))
		})
	}
}
