package internal

import (
	"context"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// The signature referrers sign the normalized manifest of the source. They only apply to the
// transferred descriptor if that normalizes to this manifest, which is checked before anything of
// the component version is written: a version must not be tagged without its signatures, and
// signatures must not reach a target for a descriptor they do not cover.
func TestTransferNormalizedComponentVersion_ChangedDescriptorFailsBeforeWriting(t *testing.T) {
	const bindError = "descriptor of component version " + normalizedKey + " changed during transfer; source signatures do not apply"

	// A resource that stays by reference, so that the upload takes it from the discovered
	// descriptor instead of from the output of a resource transformation.
	t.Run("resource digest changed in the graph", func(t *testing.T) {
		r := require.New(t)
		f := newNormalizedSourceWith(t, func(_ *testing.T, _ *ctfRepo, desc *descriptor.Descriptor) {
			image := ociImageResource("image", normalizedVersion, "ghcr.io/acme/image:1.0.0")
			image.Digest = &descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: descriptor.OCIArtifactDigestV1,
				Value:                  digest.FromString("image").Encoded(),
			}
			desc.Component.Resources = append(desc.Component.Resources, image)
		})
		target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))

		err := f.transferWith(t, transferRun{edit: func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
			t.Helper()
			baseID := identityToTransformationID(runtime.Identity{
				descriptor.IdentityAttributeName:    normalizedComponent,
				descriptor.IdentityAttributeVersion: normalizedVersion,
			})
			component := tgd.Environment.Data[baseID].(map[string]any)["component"].(map[string]any)
			image := component["resources"].([]any)[1].(map[string]any)
			image["digest"].(map[string]any)["value"] = digest.FromString("another image").Encoded()
		}}, target)

		r.ErrorContains(err, bindError)
		f.requireNotAdded(t, target)
	})

	// The descriptor is read by the version tag, the graph from the artifact the tag resolves
	// to afterwards. If the tag moves in between, the two do not belong together.
	t.Run("version tag moved while the source was discovered", func(t *testing.T) {
		r := require.New(t)
		f := newNormalizedSource(t)
		target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))

		err := f.transferWith(t, transferRun{discovery: &retaggingRepository{
			Repository: f.source.repo,
			retag: func(ctx context.Context) error {
				desc, err := f.source.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
				if err != nil {
					return err
				}
				desc.Component.Provider.Name = "another-provider"
				return f.source.repo.AddComponentVersion(ctx, desc)
			},
		}}, target)

		r.ErrorContains(err, bindError)
		f.requireNotAdded(t, target)
		sourceStore, sourceReference := f.source.store(t, normalizedComponent, normalizedVersion)
		moved, err := sourceStore.Resolve(t.Context(), sourceReference)
		r.NoError(err)
		r.NotEqual(f.normalized.Digest, moved.Digest, "the version tag of the source must have moved")
		targetStore, _ := target.store(t, normalizedComponent, normalizedVersion)
		exists, err := targetStore.Exists(t.Context(), moved)
		r.NoError(err)
		r.False(exists, "the normalized manifest the tag moved to must not be copied")
	})
}

// retaggingRepository moves a version tag between the read of the descriptor and the resolution
// of the tag to its artifact, which is the order discovery reads a normalized source in.
type retaggingRepository struct {
	*oci.Repository
	retag func(ctx context.Context) error
}

func (r *retaggingRepository) ResolveComponentVersionDigest(ctx context.Context, component, version string) (string, error) {
	if err := r.retag(ctx); err != nil {
		return "", err
	}
	return r.Repository.ResolveComponentVersionDigest(ctx, component, version)
}

// The normalized manifest is copied unchanged, with or without the component index as its
// subject. A target that tracks referrers the other way cannot hold it, which has to fail before
// the first write instead of leaving a version behind that the target does not list as it lists
// its other versions.
func TestTransferNormalizedComponentVersion_ReferrerTrackingPolicy(t *testing.T) {
	tests := []struct {
		name           string
		source, target oci.ReferrerTrackingPolicy
		wantErr        string
	}{
		{
			name:   "source and target track by index and subject",
			source: oci.ReferrerTrackingPolicyByIndexAndSubject,
			target: oci.ReferrerTrackingPolicyByIndexAndSubject,
		},
		{
			name:    "source tracks by index and subject, target does not",
			source:  oci.ReferrerTrackingPolicyByIndexAndSubject,
			target:  oci.ReferrerTrackingPolicyNone,
			wantErr: "has a subject, but the referrer tracking policy of the target requires no subject",
		},
		{
			name:    "target tracks by index and subject, source does not",
			source:  oci.ReferrerTrackingPolicyNone,
			target:  oci.ReferrerTrackingPolicyByIndexAndSubject,
			wantErr: "has no subject, but the referrer tracking policy of the target requires the component index as subject",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			f := newNormalizedSource(t, oci.WithReferrerTrackingPolicy(tt.source))
			target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized), oci.WithReferrerTrackingPolicy(tt.target))

			err := f.transfer(t, target)

			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.ErrorContains(err, normalizedKey)
				f.requireNotAdded(t, target)
				return
			}
			r.NoError(err)
			f.requireCopied(t, target, f.signatures)
			f.requireOwnAccessReferrers(t, target, 1)
			// The subject is copied with the manifest, and the target lists the version by it.
			versions, err := target.repo.ListComponentVersions(t.Context(), normalizedComponent)
			r.NoError(err)
			r.Equal([]string{normalizedVersion}, versions)
		})
	}
}

// A target that writes the default layout cannot hold the normalized manifest, so nothing there
// is to sign. The transfer still succeeds, as a transfer into such a target always did: the
// component version is built from its descriptor and the signature referrers stay behind, which
// the user is told together with how to keep them.
func TestTransferNormalizedComponentVersion_DefaultLayoutTargetLeavesSignaturesBehind(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSource(t)
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutV2))

	r.NoError(f.transfer(t, target))

	r.Contains(target.logs.String(), "level=WARN")
	r.Contains(target.logs.String(), "signature referrers of the normalized source are not carried")
	r.Contains(target.logs.String(), "componentVersionLayout: normalized")
	r.Contains(target.logs.String(), normalizedComponent)

	storedLayout, err := target.repo.GetComponentVersionLayout(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutDefault), storedLayout)
	targetStore, _ := target.store(t, normalizedComponent, normalizedVersion)
	for _, desc := range append(f.signatures, f.normalized) {
		exists, err := targetStore.Exists(ctx, desc)
		r.NoError(err)
		r.False(exists, "%s (%s) of the normalized layout must not exist on the target", desc.Digest, desc.ArtifactType)
	}

	got, err := target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Len(got.Component.Resources, 1)
	_, _, err = target.repo.GetLocalResource(ctx, normalizedComponent, normalizedVersion, runtime.Identity{"name": normalizedResource})
	r.NoError(err)
}
