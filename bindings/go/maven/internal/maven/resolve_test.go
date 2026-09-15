package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
)

// metadataServer serves body at path and 404 everywhere else.
func metadataServer(t *testing.T, path, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == path {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func filenames(refs []FileRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Filename)
	}
	return out
}

func TestResolve_Release_SingleFile(t *testing.T) {
	m := newMaven(v2alpha1.Artifact{Extension: "jar"})
	refs, err := NewClient(nil).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.2.3.jar"}, filenames(refs))
}

func TestResolve_Release_SeveralFiles_InSpecOrder(t *testing.T) {
	m := newMaven(
		v2alpha1.Artifact{Extension: "pom"},
		v2alpha1.Artifact{Extension: "jar"},
		v2alpha1.Artifact{Extension: "jar", Classifier: "sources"},
	)
	refs, err := NewClient(nil).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.2.3.pom", "lib-1.2.3.jar", "lib-1.2.3-sources.jar"}, filenames(refs))
	assert.Equal(t, "https://r/maven2/com/example/lib/1.2.3/lib-1.2.3-sources.jar", refs[2].URL)
}

func TestResolve_Snapshot_TimestampedFilenames(t *testing.T) {
	srv := metadataServer(t, "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml", snapMeta)
	m := newMaven(v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "jar", Classifier: "sources"})
	m.RepoURL = srv.URL + "/maven2"
	m.Version = "1.0-SNAPSHOT"
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.0-20240101.120000-3.jar", "lib-1.0-20240101.120000-3-sources.jar"}, filenames(refs))
	// directory keeps the base version
	assert.Equal(t, srv.URL+"/maven2/com/example/lib/1.0-SNAPSHOT/lib-1.0-20240101.120000-3.jar", refs[0].URL)
}

func TestResolve_Snapshot_UnlistedFileUsesTimestampFallback(t *testing.T) {
	srv := metadataServer(t, "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml", snapMeta)
	m := newMaven(v2alpha1.Artifact{Extension: "zip", Classifier: "dist"})
	m.RepoURL = srv.URL + "/maven2"
	m.Version = "1.0-SNAPSHOT"
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.0-20240101.120000-3-dist.zip"}, filenames(refs))
}

func TestResolve_Snapshot_NoMetadataKeepsLiteralName(t *testing.T) {
	srv := metadataServer(t, "/nothing", "")
	m := newMaven(v2alpha1.Artifact{Extension: "jar"})
	m.RepoURL = srv.URL + "/maven2"
	m.Version = "1.0-SNAPSHOT"
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.0-SNAPSHOT.jar"}, filenames(refs))
}

func TestResolve_Snapshot_MetadataServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	m := newMaven(v2alpha1.Artifact{Extension: "jar"})
	m.RepoURL = srv.URL + "/maven2"
	m.Version = "1.0-SNAPSHOT"
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	require.ErrorContains(t, err, "403")
}

func TestResolve_Snapshot_PoisonedMetadata_Errors(t *testing.T) {
	t.Run("poisoned snapshotVersion value", func(t *testing.T) {
		srv := metadataServer(t, "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml", `<metadata>
  <versioning>
    <snapshotVersions>
      <snapshotVersion><extension>jar</extension><value>../../../evil</value></snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>`)
		m := newMaven(v2alpha1.Artifact{Extension: "jar"})
		m.RepoURL = srv.URL + "/maven2"
		m.Version = "1.0-SNAPSHOT"
		_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
		require.ErrorContains(t, err, "unsafe value")
	})
	t.Run("poisoned timestamp", func(t *testing.T) {
		srv := metadataServer(t, "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml", `<metadata>
  <versioning>
    <snapshot><timestamp>../evil</timestamp><buildNumber>3</buildNumber></snapshot>
  </versioning>
</metadata>`)
		m := newMaven(v2alpha1.Artifact{Extension: "jar"})
		m.RepoURL = srv.URL + "/maven2"
		m.Version = "1.0-SNAPSHOT"
		_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
		require.ErrorContains(t, err, "unsafe value")
	})
}

const artifactMeta = `<metadata><versioning><latest>1.2.3</latest><release>1.2.2</release></versioning></metadata>`

func TestResolve_LATEST(t *testing.T) {
	srv := metadataServer(t, "/maven2/com/example/lib/maven-metadata.xml", artifactMeta)
	m := newMaven(v2alpha1.Artifact{Extension: "jar"})
	m.RepoURL = srv.URL + "/maven2"
	m.Version = "LATEST"
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.2.3.jar"}, filenames(refs))
}

func TestResolve_RELEASE(t *testing.T) {
	srv := metadataServer(t, "/maven2/com/example/lib/maven-metadata.xml", artifactMeta)
	m := newMaven(v2alpha1.Artifact{Extension: "jar"})
	m.RepoURL = srv.URL + "/maven2"
	m.Version = "RELEASE"
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib-1.2.2.jar"}, filenames(refs))
}

func TestResolve_LATEST_Errors(t *testing.T) {
	t.Run("no metadata", func(t *testing.T) {
		srv := metadataServer(t, "/nothing", "")
		m := newMaven(v2alpha1.Artifact{Extension: "jar"})
		m.RepoURL = srv.URL + "/maven2"
		m.Version = "LATEST"
		_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
		require.ErrorContains(t, err, "no maven-metadata.xml")
	})
	t.Run("empty version", func(t *testing.T) {
		srv := metadataServer(t, "/maven2/com/example/lib/maven-metadata.xml", `<metadata><versioning></versioning></metadata>`)
		m := newMaven(v2alpha1.Artifact{Extension: "jar"})
		m.RepoURL = srv.URL + "/maven2"
		m.Version = "LATEST"
		_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
		require.ErrorContains(t, err, `has no "LATEST" version`)
	})
}
