package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubdigest "ocm.software/open-component-model/bindings/go/github/digest"
	githubresource "ocm.software/open-component-model/bindings/go/github/repository/resource"
	githubaccess "ocm.software/open-component-model/bindings/go/github/spec/access"
	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// The repository the mock GitHub API below serves. Nothing here reaches github.com.
const (
	ocmOwnerRepo = "open-component-model/open-component-model"
	ocmCommit    = "f58349914e3c775747dc1ee9af1bc83db4652266"
)

// mockGitHubAPI serves the two endpoints a github download uses: the archive-link call, which
// GitHub answers with a redirect, and the archive itself. The returned URL works verbatim as a
// repoUrl because a non-github.com host makes the client treat it as GitHub Enterprise. The
// archive path is matched by suffix, since go-github prefixes an Enterprise base URL with
// /api/v3.
func mockGitHubAPI(t *testing.T) (repoURL string, payload []byte) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	const content = "hello world"
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "open-component-model-open-component-model-" + ocmCommit + "/README",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	payload = buf.Bytes()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/repos/"+ocmOwnerRepo+"/tarball/"+ocmCommit):
			http.Redirect(w, req, "http://"+req.Host+"/codeload", http.StatusFound)
		case req.URL.Path == "/codeload":
			_, _ = w.Write(payload)
		default:
			http.Error(w, "unexpected path "+req.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server.URL + "/" + ocmOwnerRepo, payload
}

// Transfers a gitHub resource pinned to a commit from a source CTF to an OCI registry, and
// checks it lands in the target as a localBlob holding the repository archive.
func Test_Integration_TransferGitHub_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start the target OCI registry and the GitHub API the resource is fetched from.
	registryAddr, user, password := startRegistry(t)
	repoURL, archive := mockGitHubAPI(t)

	// 2. Create a source CTF whose component has a gitHub resource pinned to a commit.
	componentName := "ocm.software/github-integration-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	sourceScheme := runtime.NewScheme()
	sourceScheme.MustRegisterScheme(oci.DefaultRepositoryScheme)
	// The default repository scheme knows only OCI and localBlob, and this test hand-builds a
	// typed *githubv1.GitHub access. The real authoring path does not need this: the CLI
	// constructor converts external accesses to runtime.Raw before writing.
	githubaccess.MustAddToScheme(sourceScheme)
	ctfRepo := createCTFRepository(t, sourceCTFPath, oci.WithScheme(sourceScheme))

	// A tgz of a repository tree is a directoryTree artifact; gitHub is the *access* type.
	sourceResource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "repo-source", Version: "1.0.0"},
		},
		Type:     "directoryTree",
		Relation: descriptor.ExternalRelation,
		Access: &githubv1.GitHub{
			Type:    runtime.NewVersionedType(githubv1.Type, githubv1.Version),
			RepoURL: repoURL,
			Commit:  ocmCommit,
		},
	}

	// Pin the digest the way the constructor does. Without it the target would just compute a
	// digest from whatever it stored, and the check below would never fail.
	digested, err := githubdigest.NewDigestProcessor().ProcessResourceDigest(t.Context(), &sourceResource, nil)
	r.NoError(err)
	r.NotNil(digested.Digest, "digest processor must pin a digest")
	pinnedDigest := digested.Digest.Value
	r.Equal(digestOf(archive).Encoded(), pinnedDigest,
		"the pinned digest must be the sha256 of the archive the API served")

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider:  descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{*digested},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 3. Build the transfer graph. CopyModeAllResources is required: the default
	//    CopyModeLocalBlobResources skips external resources, and a gitHub access is external.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotEmpty(tgd.Transformations)

	// 4. Build and execute the graph with the github resource repository.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := githubresource.NewResourceRepository()

	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify the resource landed in the target as a localBlob.
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("repo-source", gotDesc.Component.Resources[0].Name)
	r.Equal(descriptorv2.LocalBlobAccessType,
		gotDesc.Component.Resources[0].Access.GetType().Name,
		"transferred github resource must be stored as a localBlob in the target")

	// If the pinned digest changes, a signature over the source stops verifying against the
	// transferred component version.
	r.NotNil(gotDesc.Component.Resources[0].Digest, "transferred resource should carry a digest")
	r.Equal(pinnedDigest, gotDesc.Component.Resources[0].Digest.Value,
		"transferred resource must keep the digest pinned before the transfer")

	// The stored bytes must be the archive the digest was taken over.
	resourceIdentity := gotDesc.Component.Resources[0].ToIdentity()
	localBlob, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, resourceIdentity)
	r.NoError(err, "local blob should be retrievable from target repository")
	reader, err := localBlob.ReadCloser()
	r.NoError(err, "local blob should be readable")
	defer func() { r.NoError(reader.Close()) }()
	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(archive, content, "stored blob must be the exact archive the GitHub API served")
}
