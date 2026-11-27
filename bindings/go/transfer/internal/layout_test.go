package internal

import (
	"context"
	"slices"
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

// The upload of a normalized component version between OCI and CTF repositories is the fused
// transformation that copies its OCI graph. Everything else is added from the descriptor.
func TestBuildGraphDefinition_TransferNormalizedComponentVersion(t *testing.T) {
	const (
		component = "ocm.software/test"
		version   = "1.0.0"
		key       = component + ":" + version
		tagged    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		userPin   = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	)
	descriptors := map[string]*descriptor.Descriptor{key: testDescriptor(component, version, nil, nil)}
	// normalized is a source whose version tag resolves to the normalized artifact tagged.
	normalized := func() repository.ComponentVersionRepository {
		return &tagLayoutRepo{
			mockCVRepo:   mockCVRepo{descriptors: descriptors},
			tagLayout:    layout.ComponentVersionLayoutNormalized,
			tagDigest:    tagged,
			pinnedLayout: layout.ComponentVersionLayoutNormalized,
		}
	}
	withLayout := func(l layout.ComponentVersionLayout) func() repository.ComponentVersionRepository {
		return func() repository.ComponentVersionRepository {
			return &layoutCVRepo{mockCVRepo: mockCVRepo{descriptors: descriptors}, layout: l}
		}
	}

	tests := []struct {
		name string
		// source is the specification of the source repository, nil if it is unknown.
		source runtime.Typed
		target runtime.Typed
		repo   func() repository.ComponentVersionRepository
		// pin is the OCI digest the root is pinned to by the user, if any.
		pin      string
		wantType runtime.Type
		// wantDigest is the digest the fused transformation reads the source by.
		wantDigest string
	}{
		{
			name:     "default layout source is added from its descriptor to an OCI target",
			source:   testOCIRepo("ghcr.io/source"),
			target:   testOCIRepo("ghcr.io/target"),
			repo:     withLayout(layout.ComponentVersionLayoutDefault),
			wantType: ociv1alpha1.OCIAddComponentVersionV1alpha1,
		},
		{
			name:     "default layout source is added from its descriptor to a CTF target",
			source:   testCTFRepo("/tmp/source"),
			target:   testCTFRepo("/tmp/archive"),
			repo:     withLayout(layout.ComponentVersionLayoutDefault),
			wantType: ociv1alpha1.CTFAddComponentVersionV1alpha1,
		},
		{
			name:   "source without layout detection is added from its descriptor",
			source: testOCIRepo("ghcr.io/source"),
			target: testOCIRepo("ghcr.io/target"),
			repo: func() repository.ComponentVersionRepository {
				return &mockCVRepo{descriptors: descriptors}
			},
			wantType: ociv1alpha1.OCIAddComponentVersionV1alpha1,
		},
		{
			name:     "normalized source with unknown repository specification is added from its descriptor",
			target:   testOCIRepo("ghcr.io/target"),
			repo:     normalized,
			wantType: ociv1alpha1.OCIAddComponentVersionV1alpha1,
		},
		{
			name:       "normalized OCI source to an OCI target",
			source:     testOCIRepo("ghcr.io/source"),
			target:     testOCIRepo("ghcr.io/target"),
			repo:       normalized,
			wantType:   ociv1alpha1.TransferOCIComponentVersionToOCIV1alpha1,
			wantDigest: tagged,
		},
		{
			name:       "normalized OCI source to a CTF target",
			source:     testOCIRepo("ghcr.io/source"),
			target:     testCTFRepo("/tmp/archive"),
			repo:       normalized,
			wantType:   ociv1alpha1.TransferOCIComponentVersionToCTFV1alpha1,
			wantDigest: tagged,
		},
		{
			name:       "normalized CTF source to an OCI target",
			source:     testCTFRepo("/tmp/source"),
			target:     testOCIRepo("ghcr.io/target"),
			repo:       normalized,
			wantType:   ociv1alpha1.TransferCTFComponentVersionToOCIV1alpha1,
			wantDigest: tagged,
		},
		{
			name:       "normalized CTF source to a CTF target",
			source:     testCTFRepo("/tmp/source"),
			target:     testCTFRepo("/tmp/archive"),
			repo:       normalized,
			wantType:   ociv1alpha1.TransferCTFComponentVersionToCTFV1alpha1,
			wantDigest: tagged,
		},
		{
			name:       "normalized source pinned by the user is read by that pin",
			source:     testOCIRepo("ghcr.io/source"),
			target:     testOCIRepo("ghcr.io/target"),
			repo:       normalized,
			pin:        userPin,
			wantType:   ociv1alpha1.TransferOCIComponentVersionToOCIV1alpha1,
			wantDigest: userPin,
		},
		{
			name:     "normalized source that cannot resolve digests is read by its version tag",
			source:   testOCIRepo("ghcr.io/source"),
			target:   testOCIRepo("ghcr.io/target"),
			repo:     withLayout(layout.ComponentVersionLayoutNormalized),
			wantType: ociv1alpha1.TransferOCIComponentVersionToOCIV1alpha1,
		},
	}

	fused := []runtime.Type{
		ociv1alpha1.TransferOCIComponentVersionToOCIV1alpha1, ociv1alpha1.TransferOCIComponentVersionToCTFV1alpha1,
		ociv1alpha1.TransferCTFComponentVersionToOCIV1alpha1, ociv1alpha1.TransferCTFComponentVersionToCTFV1alpha1,
	}
	uploadTypes := append([]runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1}, fused...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			repo := tt.repo()
			resolver := &mockCVRepoResolver{
				specs:      map[string]runtime.Typed{key: tt.source},
				repos:      map[string]repository.ComponentVersionRepository{key: repo},
				sharedRepo: repo,
			}
			roots := testTransferRoots(component, version, tt.target, resolver)
			if tt.pin != "" {
				pin, err := repository.ParseDigestPin(tt.pin)
				r.NoError(err)
				root := roots[key]
				root.ExpectedDigest = pin
				roots[key] = root
			}

			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
			r.NoError(err)

			r.Len(tgd.Transformations, 1, "the upload is the only transformation: nothing runs after it to carry signatures")
			uploads := findTransformations(tgd, uploadTypes...)
			r.Len(uploads, 1)
			upload := uploads[0]
			r.Equal(tt.wantType, upload.Type)
			baseID := identityToTransformationID(runtime.Identity{
				descriptor.IdentityAttributeName:    component,
				descriptor.IdentityAttributeVersion: version,
			})
			r.Equal(baseID+"Upload", upload.ID)
			r.Equal("${environment."+baseID+"}", upload.Spec.Data["descriptor"])

			targetRepo, err := asUnstructured(tt.target)
			r.NoError(err)
			r.Equal(targetRepo.Data, upload.Spec.Data["repository"])
			r.NotContains(targetRepo.Data, "componentVersionLayout", "the layout is not part of the target repository specification")

			if slices.Contains(fused, tt.wantType) {
				sourceRepo, err := asUnstructured(tt.source)
				r.NoError(err)
				want := map[string]any{"repository": sourceRepo.Data, "component": component, "version": version}
				if tt.wantDigest != "" {
					want["digest"] = tt.wantDigest
				}
				r.Equal(want, upload.Spec.Data["source"])
			} else {
				r.NotContains(upload.Spec.Data, "source")
			}

			_, err = NewDefaultBuilder(nil, nil, nil, nil, "").BuildAndCheck(tgd)
			r.NoError(err, "the upload must type-check against its schema")
		})
	}
}
