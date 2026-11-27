package internal

import (
	"io"
	"slices"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Every target reads the graph of the source itself, so each of them gets the signature
// referrers and writes its own access referrer.
func TestTransferNormalizedComponentVersion_MultipleTargets(t *testing.T) {
	r := require.New(t)
	f := newNormalizedSource(t)
	targets := []*ctfRepo{
		f.provider.add(t, "/target-a", oci.WithComponentVersionLayout(oci.LayoutNormalized)),
		f.provider.add(t, "/target-b", oci.WithComponentVersionLayout(oci.LayoutNormalized)),
	}

	r.NoError(f.transfer(t, targets...))

	for _, target := range targets {
		f.requireCopied(t, target, f.signatures)
		f.requireOwnAccessReferrers(t, target, 1)
		_, err := target.repo.GetComponentVersion(t.Context(), normalizedComponent, normalizedVersion)
		r.NoError(err, "target %s", target.spec.FilePath)
	}
}

// A transfer into a target that already holds the component version must not need a read of what
// is there: the graph is copied again, which changes nothing, and another access referrer is
// written, which reads prefer over the older ones.
func TestTransferNormalizedComponentVersion_RerunIntoPopulatedTarget(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSource(t)
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))
	// signatures lists the signature referrers of the target with the subjects they refer to.
	signatures := func() map[digest.Digest][]digest.Digest {
		listed := map[digest.Digest][]digest.Digest{}
		for _, subject := range append([]ociImageSpecV1.Descriptor{f.normalized}, f.signatures...) {
			for _, artifactType := range []string{cosignSignatureArtifactType, cosignAttestationArtifactType} {
				listed[subject.Digest] = append(listed[subject.Digest], digestsOf(target.referrers(t, normalizedComponent, normalizedVersion, subject, artifactType))...)
			}
			slices.Sort(listed[subject.Digest])
		}
		return listed
	}

	r.NoError(f.transfer(t, target))
	first := f.requireOwnAccessReferrers(t, target, 1)
	transferred := signatures()
	r.Len(transferred[f.normalized.Digest], 2)

	r.NoError(f.transfer(t, target))

	f.requireCopied(t, target, f.signatures)
	r.Equal(transferred, signatures(), "the signature referrers of the target must be unchanged")
	second := f.requireOwnAccessReferrers(t, target, 2)
	r.Contains(digestsOf(second), first[0].Digest, "the access referrer of the first run must stay listed")
	got, err := target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Empty(got.Signatures)

	// The access referrers of both runs hold the same descriptor. To tell which one a read
	// selects, the source is signed, which changes its descriptor but not its normalized
	// manifest, and transferred once more.
	signed, err := f.source.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	signed.Signatures = []descriptor.Signature{{
		Name: "release",
		Digest: descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: normalizedlayout.NormalisationAlgorithm,
			Value:                  digest.FromString("signed").Encoded(),
		},
		Signature: descriptor.SignatureInfo{Algorithm: "RSASSA-PSS", Value: "abcd", MediaType: "application/vnd.ocm.signature.rsa"},
	}}
	r.NoError(f.source.repo.AddComponentVersion(ctx, signed))

	r.NoError(f.transfer(t, target))

	f.requireCopied(t, target, f.signatures)
	r.Equal(transferred, signatures(), "the signature referrers of the target must be unchanged")
	f.requireOwnAccessReferrers(t, target, 3)
	got, err = target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Len(got.Signatures, 1, "the read must return the descriptor of the access referrer written last")
	r.Equal("release", got.Signatures[0].Name)
}

// A root pinned by OCI digest is transferred from the artifact with that digest, whatever its
// version tag points at by now: the layout that selects the fused transfer, the descriptor, the
// graph with its signature referrers and the local blobs are all read from the pinned artifact.
func TestTransferNormalizedComponentVersion_PinnedSourceWhoseTagMoved(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSource(t)
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))

	// The version tag moves to an artifact in the default layout that holds another descriptor.
	defaultLayout, err := oci.NewRepository(ocictf.WithCTF(f.source.archive), oci.WithTempDir(t.TempDir()), oci.WithComponentVersionLayout(oci.LayoutV2))
	r.NoError(err)
	moved := testDescriptor(normalizedComponent, normalizedVersion, nil, nil)
	moved.Component.Provider.Name = "moved-provider"
	r.NoError(defaultLayout.AddComponentVersion(ctx, moved))
	taggedLayout, err := f.source.repo.GetComponentVersionLayout(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutDefault), taggedLayout, "the version tag of the source must point at the artifact in the default layout")

	r.NoError(f.transferWith(t, transferRun{pin: f.normalized.Digest.String()}, target))

	f.requireCopied(t, target, f.signatures)
	f.requireOwnAccessReferrers(t, target, 1)
	got, err := target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Equal("test-provider", got.Component.Provider.Name, "the descriptor must be the one of the pinned artifact")
	r.Len(got.Component.Resources, 1)
	localBlob, _, err := target.repo.GetLocalResource(ctx, normalizedComponent, normalizedVersion, runtime.Identity{"name": normalizedResource})
	r.NoError(err)
	reader, err := localBlob.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.NoError(reader.Close())
	r.Equal(f.resourceData, data)
}

// Retention or garbage collection can remove a signature whose referrers listing still names
// it. That must not block transfers of the version for good: the missing referrer is skipped with
// a warning and everything else is copied.
func TestTransferNormalizedComponentVersion_ListedSignatureWithoutManifestIsSkipped(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSource(t)
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))
	signatures, attestation := f.signatures[:2], f.signatures[2]
	r.Equal(cosignAttestationArtifactType, attestation.ArtifactType)
	r.NoError(f.source.ctf.DeleteBlob(ctx, attestation.Digest.String()))
	r.Contains(digestsOf(f.source.referrers(t, normalizedComponent, normalizedVersion, f.normalized, cosignAttestationArtifactType)), attestation.Digest,
		"the source must still list the attestation")

	r.NoError(f.transfer(t, target))

	r.Contains(target.logs.String(), "level=WARN")
	r.Contains(target.logs.String(), "skipping missing subject or referrer during OCI copy")
	r.Contains(target.logs.String(), attestation.Digest.String())

	f.requireCopied(t, target, signatures)
	f.requireOwnAccessReferrers(t, target, 1)
	r.Empty(target.referrers(t, normalizedComponent, normalizedVersion, f.normalized, cosignAttestationArtifactType), "the target must not list the missing attestation")
	_, err := target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
}
