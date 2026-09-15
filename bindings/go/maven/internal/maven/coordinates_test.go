package maven

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
)

func newMaven(artifacts ...v2alpha1.Artifact) *v2alpha1.Maven {
	return &v2alpha1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3", Artifacts: artifacts}
}

func TestMakeRef_Paths(t *testing.T) {
	tests := []struct {
		name string
		m    *v2alpha1.Maven
		a    v2alpha1.Artifact
		want string
	}{
		{
			"jar",
			newMaven(),
			v2alpha1.Artifact{Extension: "jar"},
			"https://r/maven2/com/example/lib/1.2.3/lib-1.2.3.jar",
		},
		{
			"pom",
			newMaven(),
			v2alpha1.Artifact{Extension: "pom"},
			"https://r/maven2/com/example/lib/1.2.3/lib-1.2.3.pom",
		},
		{
			"classifier and nested group id",
			&v2alpha1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example.sub", ArtifactID: "lib", Version: "1.2.3"},
			v2alpha1.Artifact{Extension: "jar", Classifier: "sources"},
			"https://r/maven2/com/example/sub/lib/1.2.3/lib-1.2.3-sources.jar",
		},
		{
			"trailing slash on repoUrl is normalised",
			&v2alpha1.Maven{RepoURL: "https://repo1.maven.org/maven2/", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"},
			v2alpha1.Artifact{Extension: "jar"},
			"https://repo1.maven.org/maven2/com/example/lib/1.2.3/lib-1.2.3.jar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := makeRef(tt.m, tt.m.Version, tt.m.Version, tt.a)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ref.URL)
		})
	}
}

func TestMakeRef_Filename(t *testing.T) {
	ref, err := makeRef(newMaven(), "1.2.3", "1.2.3", v2alpha1.Artifact{Extension: "jar", Classifier: "sources"})
	require.NoError(t, err)
	assert.Equal(t, "lib-1.2.3-sources.jar", ref.Filename)
}

func TestMakeRef_SnapshotTimestampedFilename(t *testing.T) {
	m := newMaven()
	m.Version = "1.0-SNAPSHOT"
	ref, err := makeRef(m, "1.0-SNAPSHOT", "1.0-20240101.120000-3", v2alpha1.Artifact{Extension: "jar"})
	require.NoError(t, err)
	// directory keeps baseVersion, filename uses the resolved version
	assert.Equal(t, "https://r/maven2/com/example/lib/1.0-SNAPSHOT/lib-1.0-20240101.120000-3.jar", ref.URL)
}

func TestMakeRef_RepoURLErrors(t *testing.T) {
	t.Run("invalid repoUrl", func(t *testing.T) {
		m := newMaven()
		m.RepoURL = "://bad"
		_, err := makeRef(m, "1", "1", v2alpha1.Artifact{Extension: "jar"})
		require.ErrorContains(t, err, "repoUrl")
	})
}

func TestReleaseFileName(t *testing.T) {
	assert.Equal(t, "lib-1.2.3.jar", ReleaseFileName(newMaven(), v2alpha1.Artifact{Extension: "jar"}))
	assert.Equal(t, "lib-1.2.3-sources.jar", ReleaseFileName(newMaven(), v2alpha1.Artifact{Extension: "jar", Classifier: "sources"}))
}

func TestFileURL(t *testing.T) {
	u, err := FileURL(newMaven(), "lib-1.2.3-sources.jar.asc")
	require.NoError(t, err)
	assert.Equal(t, "https://r/maven2/com/example/lib/1.2.3/lib-1.2.3-sources.jar.asc", u)
}

func TestMediaTypeFor(t *testing.T) {
	for extension, want := range map[string]string{
		"jar":        "application/java-archive",
		"pom":        "application/xml",
		"asc":        "text/plain",
		"sha1":       "text/plain",
		"unknownext": "application/octet-stream",
	} {
		assert.Equal(t, want, MediaTypeFor(extension), extension)
	}
}

func TestIsSnapshot(t *testing.T) {
	assert.True(t, IsSnapshot("1.0-SNAPSHOT"))
	assert.False(t, IsSnapshot("1.0"))
}

func TestVersionMetadataURL(t *testing.T) {
	u, err := versionMetadataURL(newMaven(), "1.0-SNAPSHOT")
	require.NoError(t, err)
	assert.Equal(t, "https://r/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml", u)
}

func TestArtifactMetadataURL(t *testing.T) {
	u, err := artifactMetadataURL(newMaven())
	require.NoError(t, err)
	assert.Equal(t, "https://r/maven2/com/example/lib/maven-metadata.xml", u)
}
