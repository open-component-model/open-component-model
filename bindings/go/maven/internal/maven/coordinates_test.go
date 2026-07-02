package maven

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
)

func ptr[T any](v T) *T { return &v }

func TestMakeRef_Paths(t *testing.T) {
	tests := []struct {
		name       string
		m          *v1.Maven
		classifier string
		extension  string
		want       string
	}{
		{
			"default jar",
			&v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"},
			"", "",
			"https://r/maven2/com/example/lib/1.2.3/lib-1.2.3.jar",
		},
		{
			"explicit extension",
			&v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"},
			"", "pom",
			"https://r/maven2/com/example/lib/1.2.3/lib-1.2.3.pom",
		},
		{
			"classifier and nested group id",
			&v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example.sub", ArtifactID: "lib", Version: "1.2.3"},
			"sources", "",
			"https://r/maven2/com/example/sub/lib/1.2.3/lib-1.2.3-sources.jar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := makeRef(tt.m, tt.m.Version, tt.m.Version, tt.classifier, tt.extension)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ref.URL)
		})
	}
}

func TestArtifactURL(t *testing.T) {
	t.Run("joins and normalizes trailing slash", func(t *testing.T) {
		u, err := ArtifactURL(&v1.Maven{
			RepoURL: "https://repo1.maven.org/maven2/", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://repo1.maven.org/maven2/com/example/lib/1.2.3/lib-1.2.3.jar", u)
	})
	t.Run("invalid repoUrl errors", func(t *testing.T) {
		_, err := ArtifactURL(&v1.Maven{RepoURL: "://bad", GroupID: "g", ArtifactID: "a", Version: "1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "repoUrl")
	})
	t.Run("relative repoUrl errors", func(t *testing.T) {
		_, err := ArtifactURL(&v1.Maven{RepoURL: "/relative/path", GroupID: "g", ArtifactID: "a", Version: "1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "absolute")
	})
	t.Run("pointer classifier and extension", func(t *testing.T) {
		u, err := ArtifactURL(&v1.Maven{
			RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3",
			Classifier: ptr("sources"), Extension: ptr("jar"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://r/maven2/com/example/lib/1.2.3/lib-1.2.3-sources.jar", u)
	})
}

func TestDefaultMediaType(t *testing.T) {
	assert.Equal(t, "application/java-archive", defaultMediaType("jar"))
	assert.Equal(t, "application/octet-stream", defaultMediaType("pom"))
}

func TestDefaultMediaTypeMime(t *testing.T) {
	// jar keeps its explicit maven media type.
	if got := defaultMediaType("jar"); got != "application/java-archive" {
		t.Fatalf("jar media type = %q", got)
	}
	// unknown extension falls back to octet-stream.
	if got := defaultMediaType("unknownext"); got != "application/octet-stream" {
		t.Fatalf("unknown media type = %q", got)
	}
	// json resolves via the stdlib mime table.
	if got := defaultMediaType("json"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("json media type = %q, want application/json...", got)
	}
}

func TestIsSnapshot(t *testing.T) {
	if !IsSnapshot("1.0-SNAPSHOT") {
		t.Fatal("want snapshot")
	}
	if IsSnapshot("1.0") {
		t.Fatal("want release")
	}
}

func TestIsFileIsPackage(t *testing.T) {
	bare := &v1.Maven{GroupID: "g", ArtifactID: "a", Version: "1"}
	assert.True(t, IsPackage(bare))
	assert.False(t, IsFile(bare))

	full := &v1.Maven{GroupID: "g", ArtifactID: "a", Version: "1", Classifier: ptr("sources"), Extension: ptr("jar")}
	assert.True(t, IsFile(full))
	assert.False(t, IsPackage(full))

	partial := &v1.Maven{GroupID: "g", ArtifactID: "a", Version: "1", Classifier: ptr("")}
	assert.False(t, IsFile(partial))
	assert.False(t, IsPackage(partial))
}

func TestMakeRef_ReleaseWithClassifier(t *testing.T) {
	m := &v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"}
	ref, err := makeRef(m, "1.2.3", "1.2.3", "sources", "jar")
	if err != nil {
		t.Fatal(err)
	}
	if ref.URL != "https://r/maven2/com/example/lib/1.2.3/lib-1.2.3-sources.jar" {
		t.Fatalf("url: %s", ref.URL)
	}
	if ref.Filename != "lib-1.2.3-sources.jar" {
		t.Fatalf("filename: %s", ref.Filename)
	}
	if ref.MediaType != "application/java-archive" {
		t.Fatalf("mediaType: %s", ref.MediaType)
	}
}

func TestMakeRef_SnapshotTimestampedFilename(t *testing.T) {
	m := &v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT"}
	ref, err := makeRef(m, "1.0-SNAPSHOT", "1.0-20240101.120000-3", "", "jar")
	if err != nil {
		t.Fatal(err)
	}
	// directory keeps baseVersion, filename uses the resolved version
	if ref.URL != "https://r/maven2/com/example/lib/1.0-SNAPSHOT/lib-1.0-20240101.120000-3.jar" {
		t.Fatalf("url: %s", ref.URL)
	}
}

func TestVersionMetadataURL(t *testing.T) {
	m := &v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT"}
	u, err := versionMetadataURL(m, "1.0-SNAPSHOT")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://r/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
		t.Fatalf("url: %s", u)
	}
}

func TestArtifactMetadataURL(t *testing.T) {
	m := &v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT"}
	u, err := artifactMetadataURL(m)
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://r/maven2/com/example/lib/maven-metadata.xml" {
		t.Fatalf("url: %s", u)
	}
}

func TestUploadMediaType(t *testing.T) {
	t.Run("default jar", func(t *testing.T) {
		assert.Equal(t, "application/java-archive", UploadMediaType(&v1.Maven{}))
	})
	t.Run("explicit extension", func(t *testing.T) {
		assert.Equal(t, "application/octet-stream", UploadMediaType(&v1.Maven{Extension: ptr("pom")}))
	})
	t.Run("explicit media type wins", func(t *testing.T) {
		assert.Equal(t, "application/x-custom", UploadMediaType(&v1.Maven{MediaType: ptr("application/x-custom")}))
	})
}
