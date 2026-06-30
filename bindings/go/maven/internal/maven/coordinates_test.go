package maven_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coordinates "ocm.software/open-component-model/bindings/go/maven/internal/maven"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
)

func TestArtifactPath(t *testing.T) {
	tests := []struct {
		name string
		m    *v1.Maven
		want string
	}{
		{"default jar", &v1.Maven{GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"},
			"com/example/lib/1.2.3/lib-1.2.3.jar"},
		{"explicit extension", &v1.Maven{GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3", Extension: "pom"},
			"com/example/lib/1.2.3/lib-1.2.3.pom"},
		{"classifier", &v1.Maven{GroupID: "com.example.sub", ArtifactID: "lib", Version: "1.2.3", Classifier: "sources"},
			"com/example/sub/lib/1.2.3/lib-1.2.3-sources.jar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, coordinates.ArtifactPath(tt.m))
		})
	}
}

func TestArtifactURL(t *testing.T) {
	t.Run("joins and normalizes trailing slash", func(t *testing.T) {
		u, err := coordinates.ArtifactURL(&v1.Maven{
			RepoURL: "https://repo1.maven.org/maven2/", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://repo1.maven.org/maven2/com/example/lib/1.2.3/lib-1.2.3.jar", u)
	})
	t.Run("invalid repoUrl errors", func(t *testing.T) {
		_, err := coordinates.ArtifactURL(&v1.Maven{RepoURL: "://bad", GroupID: "g", ArtifactID: "a", Version: "1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "repoUrl")
	})
	t.Run("relative repoUrl errors", func(t *testing.T) {
		_, err := coordinates.ArtifactURL(&v1.Maven{RepoURL: "/relative/path", GroupID: "g", ArtifactID: "a", Version: "1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "absolute")
	})
}

func TestDefaultMediaType(t *testing.T) {
	assert.Equal(t, "application/java-archive", coordinates.DefaultMediaType(&v1.Maven{}))
	assert.Equal(t, "application/octet-stream", coordinates.DefaultMediaType(&v1.Maven{Extension: "pom"}))
}

func TestDefaultMediaTypeMime(t *testing.T) {
	// jar keeps its explicit maven media type.
	if got := coordinates.DefaultMediaType(&v1.Maven{Extension: "jar"}); got != "application/java-archive" {
		t.Fatalf("jar media type = %q", got)
	}
	// unknown extension falls back to octet-stream.
	if got := coordinates.DefaultMediaType(&v1.Maven{Extension: "unknownext"}); got != "application/octet-stream" {
		t.Fatalf("unknown media type = %q", got)
	}
	// json resolves via the stdlib mime table.
	if got := coordinates.DefaultMediaType(&v1.Maven{Extension: "json"}); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("json media type = %q, want application/json...", got)
	}
}
