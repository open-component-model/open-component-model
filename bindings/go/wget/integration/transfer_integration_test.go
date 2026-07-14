package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// createCTFRepository creates a CTF-backed OCI repository at the given path.
func createCTFRepository(t *testing.T, path string) repository.ComponentVersionRepository {
	t.Helper()
	fs, err := filesystem.NewFS(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(archive)
	repo, err := oci.NewRepository(oci.WithResolver(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return repo
}

// Test_Integration_TransferWget_CTFToCTF verifies that a component with a wget-access resource is
// transferred by value: under CopyModeAllResources the URL content is downloaded and embedded as a
// local blob in the target CTF repository, making the target self-contained.
func Test_Integration_TransferWget_CTFToCTF(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	const content = "hello wget transfer integration"

	// 1. Serve the resource content over HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	// 2. Create a source CTF with a component whose single resource has a wget access.
	componentName := "ocm.software/wget-integration-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	sourceRepo := createCTFRepository(t, sourceCTFPath)

	// Serialize the wget access to a raw form so the CTF store does not need the wget access type
	// registered in its own scheme; the transfer layer re-parses it during discovery.
	wgetAccessRaw := &runtime.Raw{}
	r.NoError(wgetaccess.Scheme.Convert(&wgetv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       srv.URL + "/wget-resource",
		MediaType: "text/plain",
	}, wgetAccessRaw))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "wget-resource", Version: "1.0.0"},
					},
					Type:     "blob",
					Relation: descriptor.ExternalRelation,
					Access:   wgetAccessRaw,
				},
			},
		},
	}
	r.NoError(sourceRepo.AddComponentVersion(ctx, desc))

	// 3. Build the transfer graph with allResources so the wget resource is downloaded and embedded.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetCTFPath := t.TempDir()
	targetSpec := &ctfrepospec.Repository{
		Type:       runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath:   targetCTFPath,
		AccessMode: "readwrite|create",
	}

	tgd, err := transfer.BuildGraphDefinition(ctx,
		&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(sourceRepo, sourceSpec),
		},
	)
	r.NoError(err, "graph definition should build successfully")
	r.NotEmpty(tgd.Transformations)

	// 4. Build and execute the graph. The wget resource repository provides the download backend
	// used by the GetWget transformer; the OCI resource repository is unused for a wget-only component.
	repoProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithTempDir(t.TempDir()),
	)
	resourceRepo := wgetrepository.NewResourceRepository()
	credResolver := credentials.NewStaticCredentialsResolver(nil)

	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err, "graph should build and validate")
	r.NoError(graph.Process(ctx), "graph execution should succeed")

	// 5. Verify the target CTF now holds the resource as a self-contained local blob.
	targetRepo := createCTFRepository(t, targetCTFPath)
	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should find transferred component in target CTF")
	r.Len(gotDesc.Component.Resources, 1)

	gotResource := gotDesc.Component.Resources[0]
	r.Equal("wget-resource", gotResource.Name)
	r.True(descriptorv2.IsLocalBlob(gotResource.Access), "transferred resource access should be a local blob, got %T", gotResource.Access)

	// The wget URL must no longer be referenced anywhere in the transferred access.
	blobData, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, gotResource.ToIdentity())
	r.NoError(err, "should read back the embedded local blob")
	rc, err := blobData.ReadCloser()
	r.NoError(err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, string(got), "embedded blob content should match the served bytes")
}
