package oci_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesRelativeOCIReferenceAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"relativeOciReference versioned", runtime.NewVersionedType(v1.RelativeOCIReferenceType, v1.Version)},
		{"relativeOciReference unversioned", runtime.NewUnversionedType(v1.RelativeOCIReferenceType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := oci.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v1.RelativeOCIReference{}, obj)
		})
	}
}

// TestIsRelativeOCIReference is the broad-scheme false-positive regression guard: the
// classifier uses a narrow scheme, so it must return true only for the relative type
// (concrete, Raw, Unstructured; both spellings) and false for every other OCI access and
// nil. Probing the broad scheme would misroute every OCIImage to GetLocalResource.
func TestIsRelativeOCIReference(t *testing.T) {
	relVersioned := runtime.NewVersionedType(v1.RelativeOCIReferenceType, v1.Version)
	relUnversioned := runtime.NewUnversionedType(v1.RelativeOCIReferenceType)

	tests := []struct {
		name  string
		input runtime.Typed
		want  bool
	}{
		{"nil", nil, false},
		{"concrete versioned", &v1.RelativeOCIReference{Type: relVersioned, Reference: "ocm/value:v1"}, true},
		{"concrete unversioned", &v1.RelativeOCIReference{Type: relUnversioned, Reference: "ocm/value:v1"}, true},
		{
			name:  "raw versioned",
			input: &runtime.Raw{Type: relVersioned, Data: []byte(`{"type":"relativeOciReference/v1","reference":"ocm/value:v1"}`)},
			want:  true,
		},
		{
			name:  "raw unversioned",
			input: &runtime.Raw{Type: relUnversioned, Data: []byte(`{"type":"relativeOciReference","reference":"ocm/value:v1"}`)},
			want:  true,
		},
		{
			name: "unstructured",
			input: &runtime.Unstructured{Data: map[string]any{
				"type":      "relativeOciReference/v1",
				"reference": "ocm/value:v1",
			}},
			want: true,
		},
		{
			name:  "OCIImage is not relative",
			input: &v1.OCIImage{Type: runtime.NewVersionedType(v1.OCIImageType, v1.Version), ImageReference: "ghcr.io/x:v1"},
			want:  false,
		},
		{
			name:  "legacy ociArtifact raw is not relative",
			input: &runtime.Raw{Type: runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion), Data: []byte(`{"type":"ociArtifact/v1","imageReference":"ghcr.io/x:v1"}`)},
			want:  false,
		},
		{
			name:  "OCIImageLayer is not relative",
			input: &v1.OCIImageLayer{Type: runtime.NewVersionedType(v1.OCIImageLayerType, v1.Version)},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, oci.IsRelativeOCIReference(tt.input))
		})
	}
}
