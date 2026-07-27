package oci_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	transformerv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// staticRepoProvider returns pre-built source and target repositories, routing on
// the runtime type of the requested spec: the concrete *oci.Repository spec maps
// to the target, and the raw source spec maps to the source. This lets the real
// AddComponentVersion transformer drive the true carry path against real
// normalized-layout OCI repositories.
type staticRepoProvider struct {
	target repository.ComponentVersionRepository
	source repository.ComponentVersionRepository
}

func (p *staticRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (p *staticRepoProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials runtime.Typed) (repository.ComponentVersionRepository, error) {
	// The source spec is threaded into the step as a *runtime.Raw; the target spec
	// is the concrete *oci.Repository.
	if _, ok := repositorySpecification.(*runtime.Raw); ok {
		return p.source, nil
	}
	return p.target, nil
}

func (p *staticRepoProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

// TestTransfer_AddComponentVersion_CarriesReferrerEndToEnd exercises the true carry
// path through the AddComponentVersion transformer: it adds the component version
// to a real normalized-layout target OCI repository and then carries a cosign-type
// referrer from a real normalized-layout source, while the OCM-managed access
// referrer is skipped.
//
// Approach: rather than build and execute a full transfer graph (which requires a
// broad harness), this test drives the AddComponentVersion transformer directly
// with a step whose spec carries the source repository, using real source+target
// OCI repositories. This still exercises the genuine end-to-end carry path
// (Transform -> repo.AddComponentVersion -> CarryComponentSignatures).
func TestTransfer_AddComponentVersion_CarriesReferrerEndToEnd(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Real source repository (normalized layout) backed by an in-memory CTF store.
	srcFS, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	srcCTF := ocictf.NewFromCTF(ctf.NewFileSystemCTF(srcFS))
	src := Repository(t, ocictf.WithCTF(srcCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	// Real target repository (normalized layout) backed by an in-memory CTF store.
	dstFS, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	dstCTF := ocictf.NewFromCTF(ctf.NewFileSystemCTF(dstFS))
	dst := Repository(t, ocictf.WithCTF(dstCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	const compName, compVersion = "example.org/comp", "1.0.0"

	// Seed the SOURCE with the component version so that its normalized manifest and
	// referrers exist to be carried.
	desc := simpleDescriptor(compName, compVersion)
	r.NoError(src.AddComponentVersion(ctx, desc))

	// Push a cosign-type (non-access) referrer and an OCM access referrer onto the
	// source component manifest.
	srcRef := srcCTF.ComponentVersionReference(ctx, compName, compVersion)
	srcStore, err := srcCTF.StoreForReference(ctx, srcRef)
	r.NoError(err)
	srcManifest, err := srcStore.Resolve(ctx, srcRef)
	r.NoError(err)
	cosignReferrer := pushReferrer(t, ctx, srcStore, srcManifest, "application/vnd.dev.cosign.simplesigning.v1+json")
	accessReferrer := pushReferrer(t, ctx, srcStore, srcManifest, ocidescriptor.ArtifactTypeAccessDescriptor)

	// Build the v2 descriptor for the upload step.
	v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	r.NoError(err)

	// Wire the real transformer with a provider returning the real repos.
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&transformerv1alpha1.OCIAddComponentVersion{}, transformerv1alpha1.OCIAddComponentVersionV1alpha1)
	scheme.MustRegisterWithAlias(&transformerv1alpha1.CTFAddComponentVersion{}, transformerv1alpha1.CTFAddComponentVersionV1alpha1)

	provider := &staticRepoProvider{target: dst, source: src}
	tr := &transformer.AddComponentVersion{
		Scheme:       scheme,
		RepoProvider: provider,
	}

	// The step carries the source repository so the carry runs after the add.
	sourceSpec := &runtime.Raw{
		Type: runtime.NewVersionedType(ocispec.Type, "v1"),
		Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"source"}`),
	}
	step := &transformerv1alpha1.OCIAddComponentVersion{
		Type: runtime.NewVersionedType(transformerv1alpha1.OCIAddComponentVersionType, transformerv1alpha1.Version),
		ID:   "upload",
		Spec: &transformerv1alpha1.OCIAddComponentVersionSpec{
			Repository: ocispec.Repository{
				Type:    runtime.NewVersionedType(ocispec.Type, "v1"),
				BaseUrl: "target",
			},
			Descriptor:       v2desc,
			SourceRepository: sourceSpec,
		},
	}

	_, err = tr.Transform(ctx, step)
	r.NoError(err)

	// Resolve the target component manifest and confirm the digest matches the source
	// (normalized layout preserves the component-manifest digest).
	dstRef := dstCTF.ComponentVersionReference(ctx, compName, compVersion)
	dstStore, err := dstCTF.StoreForReference(ctx, dstRef)
	r.NoError(err)
	dstManifest, err := dstStore.Resolve(ctx, dstRef)
	r.NoError(err)
	r.Equal(srcManifest.Digest, dstManifest.Digest, "target normalized manifest digest must match source")

	// The cosign referrer must have been carried into the target.
	existsCosign, err := dstStore.Exists(ctx, cosignReferrer)
	r.NoError(err)
	r.True(existsCosign, "the cosign referrer must be carried into the target through the transfer path")

	// The OCM access referrer from the source must NOT have been copied.
	existsAccess, err := dstStore.Exists(ctx, accessReferrer)
	r.NoError(err)
	r.False(existsAccess, "the source access referrer must be skipped, not carried")
}
