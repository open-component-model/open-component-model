package internal

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLookupOCIUploadSupported(t *testing.T) {
	tests := []struct {
		name   string
		access runtime.Typed
		want   bool
	}{
		{
			name:   "pointer to LocalBlob is found",
			access: &descriptorv2.LocalBlob{},
			want:   true,
		},
		{
			name:   "pointer to OCIImage is found",
			access: &ociv1.OCIImage{},
			want:   true,
		},
		{
			name:   "pointer to Helm is found",
			access: &helmv1.Helm{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			_, ok := lookupOCIUploadSupported(tt.access)
			r.Equal(tt.want, ok, "lookupOCIUploadSupported(%T) returned unexpected result", tt.access)
		})
	}
}

func TestLookupProcessor(t *testing.T) {
	tests := []struct {
		name   string
		access runtime.Typed
		want   bool
	}{
		{
			name:   "pointer to LocalBlob is found",
			access: &descriptorv2.LocalBlob{},
			want:   true,
		},
		{
			name:   "pointer to OCIImage is found",
			access: &ociv1.OCIImage{},
			want:   true,
		},
		{
			name:   "pointer to Helm is found",
			access: &helmv1.Helm{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			_, ok := lookupProcessor(tt.access)
			r.Equal(tt.want, ok, "lookupProcessor(%T) returned unexpected result", tt.access)
		})
	}
}

func TestLookupOCIUploadSupported_ViaSchemeNewObject(t *testing.T) {
	// This test verifies that access objects created via Scheme.NewObject
	// (as done in graph.go) can be found by lookupOCIUploadSupported.
	// Scheme.NewObject returns pointers, so the lookup must handle pointer types.
	tests := []struct {
		name       string
		accessType runtime.Type
		want       bool
	}{
		{
			name:       "LocalBlob created via Scheme",
			accessType: runtime.Type{Name: "localBlob", Version: "v1"},
			want:       true,
		},
		{
			name:       "OCIImage created via Scheme",
			accessType: runtime.Type{Name: "ociArtifact", Version: "v1"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			access, err := Scheme.NewObject(tt.accessType)
			r.NoError(err, "Scheme.NewObject should succeed")

			t.Logf("Scheme.NewObject returned type: %T (reflect.TypeOf: %v)", access, reflect.TypeOf(access))

			_, ok := lookupOCIUploadSupported(access)
			r.Equal(tt.want, ok, "lookupOCIUploadSupported should find %T", access)

			_, ok = lookupProcessor(access)
			r.Equal(tt.want, ok, "lookupProcessor should find %T", access)
		})
	}
}
