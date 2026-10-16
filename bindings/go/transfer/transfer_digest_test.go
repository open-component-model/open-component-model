package transfer_test

import (
	"bytes"
	"os"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

var digestTestContent = []byte("transferred content")

// A transfer copies every resource digest exactly as recorded in the source, even a missing
// one that the target repository computes while storing the local blob. A partial digest is
// rejected by the target repository, so the transfer fails instead of completing it.
func TestTransferKeepsSourceDigest(t *testing.T) {
	value := godigest.FromBytes(digestTestContent).Encoded()
	for _, tc := range []struct {
		name    string
		digest  *descriptor.Digest
		wantErr string
	}{
		{name: "missing"},
		{name: "partial", digest: &descriptor.Digest{HashAlgorithm: "sha256", Value: value}, wantErr: "a digest must be complete or absent"},
		{name: "complete", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: value}},
		{name: "excluded from the signature", digest: descriptor.NewExcludeFromSignatureDigest()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			source, sourceSpec := newDigestTestCTF(t)
			addDigestTestComponent(t, source, "ocm.software/child", tc.digest, nil)

			target, err := transferDigestTestComponent(t, source, sourceSpec, "ocm.software/child", 0)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)

			got, err := target.GetComponentVersion(t.Context(), "ocm.software/child", "1.0.0")
			r.NoError(err)
			r.Equal(tc.digest, got.Component.Resources[0].Digest)
		})
	}
}

// A parent's reference digest covers the child descriptor including its resource digests,
// so it still matches the child after a transfer when the child resource has no digest.
func TestTransferKeepsReferenceDigestOfChildWithoutDigest(t *testing.T) {
	r := require.New(t)
	source, sourceSpec := newDigestTestCTF(t)
	child := addDigestTestComponent(t, source, "ocm.software/child", nil, nil)
	addDigestTestComponent(t, source, "ocm.software/parent", nil, []descriptor.Reference{{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
		Component:   "ocm.software/child",
		Digest:      *referenceDigest(t, child),
	}})

	target, err := transferDigestTestComponent(t, source, sourceSpec, "ocm.software/parent", transferv1alpha1.RecursiveInfinite)
	r.NoError(err)

	parent, err := target.GetComponentVersion(t.Context(), "ocm.software/parent", "1.0.0")
	r.NoError(err)
	transferred, err := target.GetComponentVersion(t.Context(), "ocm.software/child", "1.0.0")
	r.NoError(err)
	r.Nil(transferred.Component.Resources[0].Digest)
	r.Equal(parent.Component.References[0].Digest, *referenceDigest(t, transferred))
}

func newDigestTestCTF(t *testing.T) (*oci.Repository, *ctfrepospec.Repository) {
	t.Helper()
	path := t.TempDir()
	fs, err := filesystem.NewFS(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return repo, &ctfrepospec.Repository{
		Type:       runtime.NewVersionedType(ctfrepospec.Type, ctfrepospec.Version),
		FilePath:   path,
		AccessMode: "readwrite|create",
	}
}

// addDigestTestComponent adds a component version with one local blob resource whose digest
// is then replaced by digest, as recorded by a tool that did not complete it.
func addDigestTestComponent(t *testing.T, repo *oci.Repository, name string, digest *descriptor.Digest, refs []descriptor.Reference) *descriptor.Descriptor {
	t.Helper()
	r := require.New(t)
	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data", Version: "1.0.0"}},
		Type:        "plainText",
		Relation:    descriptor.LocalRelation,
		Access: &descriptorv2.LocalBlob{
			Type:           runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			LocalReference: godigest.FromBytes(digestTestContent).String(),
			MediaType:      "text/plain",
		},
	}
	added, err := repo.AddLocalResource(t.Context(), name, "1.0.0", res, inmemory.New(bytes.NewReader(digestTestContent)))
	r.NoError(err)
	added.Digest = digest.DeepCopy()

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
			Provider:      descriptor.Provider{Name: "test-provider"},
			Resources:     []descriptor.Resource{*added},
			References:    refs,
		},
	}
	r.NoError(repo.AddComponentVersion(t.Context(), desc))
	return desc
}

// transferDigestTestComponent transfers a component version into a new CTF and returns the
// target with the error of processing the transfer graph.
func transferDigestTestComponent(t *testing.T, source *oci.Repository, sourceSpec *ctfrepospec.Repository, name string, recursive transferv1alpha1.Recursive) (*oci.Repository, error) {
	t.Helper()
	r := require.New(t)
	target, targetSpec := newDigestTestCTF(t)
	tgd, err := transfer.BuildGraphDefinition(t.Context(), &transferv1alpha1.Config{Recursive: recursive}, nil, transfer.Mapping{
		Components: []transfer.ComponentID{{Component: name, Version: "1.0.0"}},
		Target:     targetSpec,
		Resolver:   transfer.NewRepositoryResolver(source, sourceSpec),
	})
	r.NoError(err)
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	graph, err := transfer.NewDefaultBuilder(repoProvider, ociresource.NewResourceRepository(nil), nil).BuildAndCheck(tgd)
	r.NoError(err)
	return target, graph.Process(t.Context())
}

// referenceDigest computes a reference digest the way the constructor does.
func referenceDigest(t *testing.T, desc *descriptor.Descriptor) *descriptor.Digest {
	t.Helper()
	normalised, err := normalisation.Normalisations.Normalise(desc, v4alpha1.Algorithm)
	require.NoError(t, err)
	return &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: v4alpha1.Algorithm,
		Value:                  godigest.FromBytes(normalised).Encoded(),
	}
}
