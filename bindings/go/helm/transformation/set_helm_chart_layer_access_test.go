package transformation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/helm/transformation"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newChartLayerAccessTestResource(t *testing.T, digest *descv2.Digest) *descv2.Resource {
	t.Helper()
	r := require.New(t)

	helmAccess := v1.Helm{
		Type:           runtime.NewVersionedType(v1.LegacyType, "v1"),
		HelmRepository: "https://example.com/charts",
		HelmChart:      "mychart",
		Version:        "0.1.0",
	}
	rawAccess := runtime.Raw{}
	r.NoError(access.Scheme.Convert(&helmAccess, &rawAccess))

	return &descv2.Resource{
		ElementMeta: descv2.ElementMeta{
			ObjectMeta: descv2.ObjectMeta{
				Name:    "mychart",
				Version: "0.1.0",
				Labels:  []descv2.Label{{Name: "team", Value: []byte(`"charts"`)}},
			},
			ExtraIdentity: runtime.Identity{"arch": "any"},
		},
		SourceRefs: []descv2.SourceRef{{IdentitySelector: map[string]string{"name": "mysource"}}},
		Type:       "helmChart",
		Relation:   descv2.ExternalRelation,
		Access:     &rawAccess,
		Digest:     digest,
	}
}

func TestSetHelmChartLayerAccess_Transform(t *testing.T) {
	t.Parallel()

	layer := v1alpha1.ChartLayer{
		MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		Digest:    "sha256:" + strings.Repeat("a", 64),
		Size:      1234,
	}
	genericDigest := func(hex string) *descv2.Digest {
		return &descv2.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  hex,
		}
	}
	excluded := &descv2.Digest{
		HashAlgorithm:          "NO-DIGEST",
		NormalisationAlgorithm: "EXCLUDE-FROM-SIGNATURE",
		Value:                  "NO-DIGEST",
	}

	tests := []struct {
		name           string
		imageReference string
		layerDigest    string
		digest         *descv2.Digest
		wantRef        string
		wantErr        string
	}{
		{
			name:           "tag stripped",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			digest:         genericDigest(strings.Repeat("a", 64)),
			wantRef:        "ghcr.io/acme/charts/mychart",
		},
		{
			name:           "tag and digest stripped, scheme kept",
			imageReference: "http://localhost:5000/charts/mychart:0.1.0@sha256:" + strings.Repeat("b", 64),
			digest:         genericDigest(strings.Repeat("a", 64)),
			wantRef:        "http://localhost:5000/charts/mychart",
		},
		{
			name:           "exclusion marker is not compared",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			digest:         excluded,
			wantRef:        "ghcr.io/acme/charts/mychart",
		},
		{
			name:           "a missing digest stays missing",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			wantRef:        "ghcr.io/acme/charts/mychart",
		},
		{
			name:           "digest mismatch",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			digest:         genericDigest(strings.Repeat("c", 64)),
			wantErr:        "does not match the recorded digest",
		},
		{
			name:           "malformed recorded digest",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			digest:         genericDigest("nope"),
			wantErr:        "cannot read the recorded digest of resource mychart",
		},
		{
			name:           "digest without normalisation algorithm is compared",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			digest:         &descv2.Digest{HashAlgorithm: "SHA-256", Value: strings.Repeat("c", 64)},
			wantErr:        "does not match the recorded digest",
		},
		{
			name:           "manifest digest",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			digest:         &descv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: strings.Repeat("a", 64)},
			wantErr:        "the recorded ociArtifactDigest/v1 digest of resource mychart does not name the chart archive",
		},
		{
			name:    "empty image reference",
			digest:  genericDigest(strings.Repeat("a", 64)),
			wantErr: "spec.imageReference is required",
		},
		{
			name:           "no repository",
			imageReference: "sha256:" + strings.Repeat("b", 64),
			digest:         genericDigest(strings.Repeat("a", 64)),
			wantErr:        `spec.imageReference "sha256:` + strings.Repeat("b", 64) + `" has no repository`,
		},
		{
			name:           "invalid layer digest",
			imageReference: "ghcr.io/acme/charts/mychart:0.1.0",
			layerDigest:    "nope",
			digest:         genericDigest(strings.Repeat("a", 64)),
			wantErr:        `invalid spec.chartLayer.digest "nope"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			chartLayer := layer
			if tc.layerDigest != "" {
				chartLayer.Digest = tc.layerDigest
			}
			input := newChartLayerAccessTestResource(t, tc.digest)
			want := input.DeepCopy()

			transform := &transformation.SetHelmChartLayerAccess{Scheme: v1alpha1.Scheme}
			result, err := transform.Transform(t.Context(), &v1alpha1.SetHelmChartLayerAccess{
				Type: v1alpha1.SetHelmChartLayerAccessV1alpha1,
				ID:   "set-chart-layer-access",
				Spec: &v1alpha1.SetHelmChartLayerAccessSpec{
					Resource:       input,
					ImageReference: tc.imageReference,
					ChartLayer:     chartLayer,
				},
			})
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)

			out, ok := result.(*v1alpha1.SetHelmChartLayerAccess)
			r.True(ok)
			r.NotNil(out.Output)
			r.NotNil(out.Output.Resource)
			got := out.Output.Resource

			var layerAccess ociv1.OCIImageLayer
			r.NoError(ociaccess.Scheme.Convert(got.Access, &layerAccess))
			r.Equal("ociBlob/v1", layerAccess.Type.String())
			r.Equal(tc.wantRef, layerAccess.Reference)
			r.Equal(layer.MediaType, layerAccess.MediaType)
			r.Equal(layer.Digest, layerAccess.Digest.String())
			r.Equal(layer.Size, layerAccess.Size)
			r.Equal(tc.imageReference, layerAccess.ImageReference)

			r.Equal(want.Name, got.Name)
			r.Equal(want.Version, got.Version)
			r.Equal(want.Type, got.Type)
			r.Equal(want.Relation, got.Relation)
			r.Equal(want.Labels, got.Labels)
			r.Equal(want.ExtraIdentity, got.ExtraIdentity)
			r.Equal(want.SourceRefs, got.SourceRefs)
			r.Equal(tc.digest, got.Digest)

			var inputAccess v1.Helm
			r.NoError(access.Scheme.Convert(input.Access, &inputAccess))
			r.Equal("mychart", inputAccess.HelmChart)
			r.Equal(want.Access, input.Access)
		})
	}
}

func TestSetHelmChartLayerAccess_InvalidStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		step    runtime.Typed
		wantErr string
		// wrapped is set when wantErr is only the prefix of an error that wraps the scheme's error.
		wrapped bool
	}{
		{
			name:    "fails when the step cannot be converted",
			step:    &runtime.Raw{Data: []byte(`{"type":"Unknown/v1"}`)},
			wantErr: "failed converting generic transformation to set helm chart layer access transformation: ",
			wrapped: true,
		},
		{
			name:    "fails when spec is nil",
			step:    &v1alpha1.SetHelmChartLayerAccess{Type: v1alpha1.SetHelmChartLayerAccessV1alpha1},
			wantErr: "spec is required for set helm chart layer access transformation",
		},
		{
			name: "fails when spec.resource is nil",
			step: &v1alpha1.SetHelmChartLayerAccess{
				Type: v1alpha1.SetHelmChartLayerAccessV1alpha1,
				Spec: &v1alpha1.SetHelmChartLayerAccessSpec{ImageReference: "ghcr.io/acme/charts/mychart:0.1.0"},
			},
			wantErr: "spec.resource is required for set helm chart layer access transformation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			transform := &transformation.SetHelmChartLayerAccess{Scheme: v1alpha1.Scheme}
			result, err := transform.Transform(t.Context(), tc.step)
			if tc.wrapped {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.EqualError(err, tc.wantErr)
			}
			r.Nil(result)
		})
	}
}
