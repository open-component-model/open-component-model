// Step 7: Transferring Component Versions
//
// What you'll learn:
//   - Building a transfer graph that describes how to move a component version
//   - Executing the graph to transfer a component from one CTF repository to another
//   - Using the ComponentVersionRepositoryResolver interface for component discovery
//   - Verifying the transferred component version in the target repository

package examples

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
)

// TestExample_TransferCTFtoCTF demonstrates transferring a component version
// with a local blob resource from one CTF repository to another.
//
// The transfer works in two phases:
//  1. Build a transformation graph that describes the transfer plan
//  2. Execute the graph to perform the actual data movement
func TestExample_TransferCTFtoCTF(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	component := "acme.org/transfer-example"
	version := "1.0.0"
	resourceContent := []byte("payload to transfer")

	// --- Set up the source CTF repository with a component version ---

	sourcePath := t.TempDir()
	sourceSpec := newCTFSpecAt(t, sourcePath)
	sourceRepo := ctfSpecToRepo(t, sourceSpec)

	res := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "my-resource", Version: version},
		},
		Type: "plainText",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(resourceContent).String(),
			MediaType:      "text/plain",
		},
	}

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version},
			},
			Resources: []descriptor.Resource{*res},
		},
	}

	b := inmemory.New(bytes.NewReader(resourceContent))
	newRes, err := sourceRepo.AddLocalResource(ctx, component, version, res, b)
	r.NoError(err)
	desc.Component.Resources[0] = *newRes
	r.NoError(sourceRepo.AddComponentVersion(ctx, desc))

	// --- Build the transfer graph ---

	// The fromSpec tells the transfer where to find the source component.
	fromRef := &compref.Ref{
		Repository: sourceSpec,
		Component:  component,
		Version:    version,
	}

	// The target is another CTF repository (writable so the transfer can store data).
	targetSpec := newCTFSpecAt(t, t.TempDir())
	targetSpec.AccessMode = ctfrepospec.AccessModeReadWrite

	// The resolver helps the transfer discover component versions in the source.
	resolver := &singleRepoResolver{repo: sourceRepo, spec: sourceSpec}

	tgd, err := transfer.BuildGraphDefinition(ctx, fromRef, targetSpec, resolver)
	r.NoError(err)
	r.NotEmpty(tgd.Transformations)

	// --- Execute the transfer ---

	repoProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithTempDir(t.TempDir()),
	)
	resourceRepo := resource.NewResourceRepository(nil)

	builder := transfer.NewDefaultBuilder(repoProvider, resourceRepo, nil)
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// --- Verify the component version arrived in the target ---

	targetRepo := ctfSpecToRepo(t, targetSpec)
	got, err := targetRepo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	r.Equal(component, got.Component.Name)
	r.Equal(version, got.Component.Version)
	r.Len(got.Component.Resources, 1)
	r.Equal("my-resource", got.Component.Resources[0].Name)
}

// --- helpers ---

// newCTFSpecAt creates a CTF repository specification for the given directory path.
func newCTFSpecAt(t *testing.T, path string) *ctfrepospec.Repository {
	t.Helper()
	return &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: path,
	}
}

// ctfSpecToRepo opens an existing CTF repository from a spec.
func ctfSpecToRepo(t *testing.T, spec *ctfrepospec.Repository) *oci.Repository {
	t.Helper()
	r := require.New(t)
	fs, err := filesystem.NewFS(spec.FilePath, os.O_RDWR|os.O_CREATE)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	return repo
}

// singleRepoResolver is a minimal ComponentVersionRepositoryResolver that
// always returns the same repository. Suitable for single-source transfers.
type singleRepoResolver struct {
	repo repository.ComponentVersionRepository
	spec runtime.Typed
}

func (r *singleRepoResolver) GetRepositorySpecificationForComponent(_ context.Context, _, _ string) (runtime.Typed, error) {
	return r.spec, nil
}

func (r *singleRepoResolver) GetComponentVersionRepositoryForSpecification(_ context.Context, _ runtime.Typed) (repository.ComponentVersionRepository, error) {
	return r.repo, nil
}

func (r *singleRepoResolver) GetComponentVersionRepositoryForComponent(_ context.Context, _, _ string) (repository.ComponentVersionRepository, error) {
	return r.repo, nil
}

var _ resolvers.ComponentVersionRepositoryResolver = (*singleRepoResolver)(nil)
