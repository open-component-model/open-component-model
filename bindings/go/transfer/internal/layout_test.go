package internal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// layoutCVRepo is a source repository that reports a fixed storage layout for its component versions.
type layoutCVRepo struct {
	mockCVRepo
	layout layout.ComponentVersionLayout
}

var _ repository.ComponentVersionLayoutDetector = (*layoutCVRepo)(nil)

func (m *layoutCVRepo) GetComponentVersionLayout(context.Context, string, string) (string, error) {
	return string(m.layout), nil
}

func findTransformations(tgd *transformv1alpha1.TransformationGraphDefinition, types ...runtime.Type) []transformv1alpha1.GenericTransformation {
	var found []transformv1alpha1.GenericTransformation
	for _, tr := range tgd.Transformations {
		for _, typ := range types {
			if tr.Type == typ {
				found = append(found, tr)
			}
		}
	}
	return found
}

func TestBuildGraphDefinition_ComponentVersionLayout(t *testing.T) {
	const (
		normalized = layout.ComponentVersionLayoutNormalized
		v2         = layout.ComponentVersionLayoutV2
	)
	tests := []struct {
		name         string
		configured   layout.ComponentVersionLayout
		target       runtime.Typed
		sourceLayout layout.ComponentVersionLayout
		wantLayout   any
		wantCarry    *runtime.Type
	}{
		{
			name:         "v2 source is written with the default layout and no carry",
			target:       testOCIRepo("ghcr.io/target"),
			sourceLayout: v2,
		},
		{
			name:         "normalized source is inherited by an OCI target",
			target:       testOCIRepo("ghcr.io/target"),
			sourceLayout: normalized,
			wantLayout:   string(normalized),
			wantCarry:    &ociv1alpha1.OCICarryComponentSignaturesV1alpha1,
		},
		{
			name:         "normalized source is inherited by a CTF target",
			target:       testCTFRepo("/tmp/archive"),
			sourceLayout: normalized,
			wantLayout:   string(normalized),
			wantCarry:    &ociv1alpha1.CTFCarryComponentSignaturesV1alpha1,
		},
		{
			name:         "configured v2 overrides a normalized source",
			configured:   v2,
			target:       testOCIRepo("ghcr.io/target"),
			sourceLayout: normalized,
			wantLayout:   string(v2),
		},
		{
			name:         "configured normalized applies to a v2 source",
			configured:   normalized,
			target:       testOCIRepo("ghcr.io/target"),
			sourceLayout: v2,
			wantLayout:   string(normalized),
			wantCarry:    &ociv1alpha1.OCICarryComponentSignaturesV1alpha1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", nil, nil)
			key := "ocm.software/test:1.0.0"
			repo := &layoutCVRepo{mockCVRepo: mockCVRepo{descriptors: map[string]*descriptor.Descriptor{key: desc}}, layout: tt.sourceLayout}
			resolver := &mockCVRepoResolver{
				specs:      map[string]runtime.Typed{key: testOCIRepo("ghcr.io/source")},
				repos:      map[string]repository.ComponentVersionRepository{key: repo},
				sharedRepo: repo,
			}
			roots := testTransferRoots("ocm.software/test", "1.0.0", tt.target, resolver)

			tgd, err := BuildGraphDefinition(t.Context(), roots,
				transferv1alpha1.Config{ComponentVersionLayout: tt.configured}, nil)
			r.NoError(err)

			uploads := findTransformations(tgd, ociv1alpha1.OCIAddComponentVersionV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1)
			r.Len(uploads, 1)
			uploadRepo, ok := uploads[0].Spec.Data["repository"].(map[string]any)
			r.True(ok)
			r.Equal(tt.wantLayout, uploadRepo["componentVersionLayout"])

			carries := findTransformations(tgd, ociv1alpha1.OCICarryComponentSignaturesV1alpha1, ociv1alpha1.CTFCarryComponentSignaturesV1alpha1)
			if tt.wantCarry == nil {
				r.Empty(carries)
				return
			}
			r.Len(carries, 1)
			r.Equal(*tt.wantCarry, carries[0].Type)
			r.Equal(uploads[0].Spec.Data["repository"], carries[0].Spec.Data["repository"])

			_, err = NewDefaultBuilder(nil, nil, nil, nil).BuildAndCheck(tgd)
			r.NoError(err, "the carry node must type-check against the upload node it depends on")
		})
	}
}
