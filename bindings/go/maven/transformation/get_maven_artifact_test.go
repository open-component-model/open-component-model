package transformation

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/maven/repository/resource"
	"ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/maven/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockMavenRepo serves com.example:lib:1.2.3 as a jar with a detached
// signature and a pom, and records the Authorization header it last saw.
func mockMavenRepo(t *testing.T) (baseURL string, lastAuth *string) {
	t.Helper()
	var auth string
	files := map[string]string{
		"/maven2/com/example/lib/1.2.3/lib-1.2.3.jar":     "JARDATA",
		"/maven2/com/example/lib/1.2.3/lib-1.2.3.jar.asc": "SIGDATA",
		"/maven2/com/example/lib/1.2.3/lib-1.2.3.pom":     "POMDATA",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if data, ok := files[r.URL.Path]; ok {
			_, _ = w.Write([]byte(data))
			return
		}
		http.NotFound(w, r) // .sha1 siblings 404 -> soft pass
	}))
	t.Cleanup(server.Close)
	return server.URL + "/maven2", &auth
}

func mavenV2Resource(t *testing.T, repoURL, version string, artifacts ...v2alpha1.Artifact) *v2.Resource {
	t.Helper()
	mavenAccess := &v2alpha1.Maven{
		Type:       runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version),
		RepoURL:    repoURL,
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    version,
		Artifacts:  artifacts,
	}
	raw := runtime.Raw{}
	require.NoError(t, access.Scheme.Convert(mavenAccess, &raw))
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{Name: "lib", Version: version},
		},
		Type:   "mavenArtifact",
		Access: &raw,
	}
}

func tgzNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, h.Name)
	}
	return names
}

func TestGetMavenArtifact_Transform(t *testing.T) {
	repoURL, _ := mockMavenRepo(t)

	transformer := &GetMavenArtifact{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	outDir := t.TempDir()
	step := &v1alpha1.GetMavenArtifact{
		Type: v1alpha1.GetMavenArtifactV1alpha1,
		ID:   "get-maven-artifact",
		Spec: &v1alpha1.GetMavenArtifactSpec{
			Resource:   mavenV2Resource(t, repoURL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "pom"}),
			OutputPath: outDir,
		},
	}

	result, err := transformer.Transform(t.Context(), step)
	require.NoError(t, err)

	var transformed v1alpha1.GetMavenArtifact
	require.NoError(t, v1alpha1.Scheme.Convert(result, &transformed))
	require.NotNil(t, transformed.Output)

	require.NotEmpty(t, transformed.Output.File.URI)
	contentPath := strings.TrimPrefix(transformed.Output.File.URI, "file://")
	t.Cleanup(func() { _ = os.Remove(contentPath) })
	assert.Equal(t, outDir, filepath.Dir(contentPath), "the archive must land in the spec's outputPath")
	assert.Equal(t, "application/x-tgz", transformed.Output.File.MediaType)

	assert.Equal(t, []string{"lib-1.2.3.jar", "lib-1.2.3.jar.asc", "lib-1.2.3.pom"}, tgzNames(t, contentPath),
		"the archive must hold the listed files in spec order with the signature after its file")

	// The output resource is what the Add node's digest propagation reads, so
	// the full spec resource must survive the scheme round-trip intact.
	assert.Equal(t, step.Spec.Resource, transformed.Output.Resource)
}

// TestGetMavenArtifact_Transform_RemovesOutputOnFailure pins that a failing
// download does not leave the buffered output file behind: the graph's cleanup
// node never sees a file that did not reach a consuming node's spec.
func TestGetMavenArtifact_Transform_RemovesOutputOnFailure(t *testing.T) {
	repoURL, _ := mockMavenRepo(t)
	outDir := t.TempDir()

	transformer := &GetMavenArtifact{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	// The mock only serves 1.2.3, so any other version 404s.
	_, err := transformer.Transform(t.Context(), &v1alpha1.GetMavenArtifact{
		Type: v1alpha1.GetMavenArtifactV1alpha1,
		ID:   "get-maven-artifact",
		Spec: &v1alpha1.GetMavenArtifactSpec{
			Resource:   mavenV2Resource(t, repoURL, "9.9.9", v2alpha1.Artifact{Extension: "jar"}),
			OutputPath: outDir,
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "error downloading maven artifact")

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed download must not leave the output file behind")
}

// stubResolver is a credentials.Resolver test double.
type stubResolver struct {
	typed runtime.Typed
	err   error
	calls int
}

func (s *stubResolver) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	s.calls++
	return s.typed, s.err
}

// TestGetMavenArtifact_Transform_ResolveCredentials pins the credential policy:
// ErrNotFound downgrades to an anonymous download, any other resolver error
// fails the transform, and resolved credentials reach the download.
func TestGetMavenArtifact_Transform_ResolveCredentials(t *testing.T) {
	step := func(repoURL string) *v1alpha1.GetMavenArtifact {
		return &v1alpha1.GetMavenArtifact{
			Type: v1alpha1.GetMavenArtifactV1alpha1,
			ID:   "get-maven-artifact",
			Spec: &v1alpha1.GetMavenArtifactSpec{
				Resource: mavenV2Resource(t, repoURL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}),
			},
		}
	}
	cleanup := func(t *testing.T, result runtime.Typed) {
		var transformed v1alpha1.GetMavenArtifact
		require.NoError(t, v1alpha1.Scheme.Convert(result, &transformed))
		t.Cleanup(func() { _ = os.Remove(strings.TrimPrefix(transformed.Output.File.URI, "file://")) })
	}

	t.Run("ErrNotFound means anonymous", func(t *testing.T) {
		repoURL, lastAuth := mockMavenRepo(t)
		resolver := &stubResolver{err: credentials.ErrNotFound}
		transformer := &GetMavenArtifact{
			Scheme:             v1alpha1.Scheme,
			ResourceRepository: resource.NewResourceRepository(),
			CredentialProvider: resolver,
		}

		result, err := transformer.Transform(t.Context(), step(repoURL))
		require.NoError(t, err, "missing credentials must downgrade to an anonymous download")
		require.Equal(t, 1, resolver.calls)
		assert.Empty(t, *lastAuth)
		cleanup(t, result)
	})

	t.Run("other resolver errors fail the transform", func(t *testing.T) {
		repoURL, _ := mockMavenRepo(t)
		transformer := &GetMavenArtifact{
			Scheme:             v1alpha1.Scheme,
			ResourceRepository: resource.NewResourceRepository(),
			CredentialProvider: &stubResolver{err: assert.AnError},
		}

		_, err := transformer.Transform(t.Context(), step(repoURL))
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("resolved credentials reach the download", func(t *testing.T) {
		repoURL, lastAuth := mockMavenRepo(t)
		transformer := &GetMavenArtifact{
			Scheme:             v1alpha1.Scheme,
			ResourceRepository: resource.NewResourceRepository(),
			CredentialProvider: &stubResolver{typed: &credsv1.MavenCredentials{
				Type:          runtime.NewVersionedType(credsv1.MavenCredentialsType, credsv1.Version),
				IdentityToken: "secret-token",
			}},
		}

		result, err := transformer.Transform(t.Context(), step(repoURL))
		require.NoError(t, err)
		assert.Equal(t, "Bearer secret-token", *lastAuth, "the resolved token must authenticate the download")
		cleanup(t, result)
	})
}

func TestGetMavenArtifact_Transform_RequiresSpec(t *testing.T) {
	transformer := &GetMavenArtifact{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	for name, tc := range map[string]struct {
		spec    *v1alpha1.GetMavenArtifactSpec
		wantErr string
	}{
		"nil spec":     {spec: nil, wantErr: "spec is required for get maven artifact transformation"},
		"nil resource": {spec: &v1alpha1.GetMavenArtifactSpec{}, wantErr: "resource is required in spec for get maven artifact transformation"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := transformer.Transform(t.Context(), &v1alpha1.GetMavenArtifact{
				Type: v1alpha1.GetMavenArtifactV1alpha1,
				Spec: tc.spec,
			})
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
