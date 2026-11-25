package internal

import (
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func digestsOf(descs []ociImageSpecV1.Descriptor) []digest.Digest {
	digests := make([]digest.Digest, len(descs))
	for i, desc := range descs {
		digests[i] = desc.Digest
	}
	return digests
}

// A normalized component version is transferred as the OCI graph it is stored as: the normalized
// manifest keeps its digest, so the signature referrers of the source stay valid on the target
// and are copied with it. The access referrer is the only part the target writes itself.
func TestTransferNormalizedComponentVersion_CTFToCTF(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSource(t)
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))
	sourceAccess := f.accessReferrers(t, f.source)
	r.Len(sourceAccess, 1)

	r.NoError(f.transfer(t, target))

	sourceStore, _ := f.source.store(t, normalizedComponent, normalizedVersion)
	targetStore, targetReference := target.store(t, normalizedComponent, normalizedVersion)

	tagged, err := targetStore.Resolve(ctx, targetReference)
	r.NoError(err)
	r.Equal(f.normalized.Digest, tagged.Digest, "the version tag must resolve to the normalized manifest of the source")

	for _, signature := range f.signatures {
		exists, err := targetStore.Exists(ctx, signature)
		r.NoError(err)
		r.True(exists, "signature referrer %s (%s) must exist on the target", signature.Digest, signature.ArtifactType)
		r.Equal(fetchAll(t, sourceStore, signature), fetchAll(t, targetStore, signature), "signature referrers must be copied byte-identical")
	}
	// The target lists them as referrers of the same subjects as the source does.
	for _, subject := range []ociImageSpecV1.Descriptor{f.normalized, f.signatures[0]} {
		for _, artifactType := range []string{cosignSignatureArtifactType, cosignAttestationArtifactType} {
			r.ElementsMatch(
				digestsOf(f.source.referrers(t, normalizedComponent, normalizedVersion, subject, artifactType)),
				digestsOf(target.referrers(t, normalizedComponent, normalizedVersion, subject, artifactType)),
				"referrers of %s with artifact type %s", subject.Digest, artifactType)
		}
	}

	exists, err := targetStore.Exists(ctx, sourceAccess[0])
	r.NoError(err)
	r.False(exists, "the access referrer of the source must not be copied")
	targetAccess := f.accessReferrers(t, target)
	r.Len(targetAccess, 1, "the target must hold exactly its own access referrer")
	r.NotEqual(sourceAccess[0].Digest, targetAccess[0].Digest)

	// Reading selects the access referrer of the target and binds it to the normalized manifest.
	got, err := target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Equal(normalizedComponent, got.Component.Name)
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
