package v1_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newMaven() *v1.Maven {
	return &v1.Maven{
		RepoURL:    "https://repo1.maven.org/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.2.3",
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, newMaven().Validate())
	})
	t.Run("missing groupId", func(t *testing.T) {
		m := newMaven()
		m.GroupID = ""
		err := m.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "groupId")
	})
	t.Run("missing artifactId", func(t *testing.T) {
		m := newMaven()
		m.ArtifactID = ""
		require.ErrorContains(t, m.Validate(), "artifactId")
	})
	t.Run("missing version", func(t *testing.T) {
		m := newMaven()
		m.Version = ""
		require.ErrorContains(t, m.Validate(), "version")
	})
	t.Run("missing repoUrl", func(t *testing.T) {
		m := newMaven()
		m.RepoURL = ""
		require.ErrorContains(t, m.Validate(), "repoUrl")
	})
	t.Run("unparseable repoUrl", func(t *testing.T) {
		m := newMaven()
		m.RepoURL = "://bad"
		require.ErrorContains(t, m.Validate(), "repoUrl")
	})
}

func TestMaven_PointerFields_JSONRoundTrip(t *testing.T) {
	// classifier explicitly empty ("") must survive as non-nil; absent must stay nil.
	in := v1.Maven{
		Type:       runtime.NewVersionedType(v1.Type, v1.Version),
		RepoURL:    "https://repo1.maven.org/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.2.3",
		Classifier: ptr(""), // explicit empty
		Extension:  ptr("jar"),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"classifier":""`) {
		t.Fatalf("explicit empty classifier should serialize, got %s", data)
	}
	var out v1.Maven
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Classifier == nil || *out.Classifier != "" {
		t.Fatalf("classifier should be non-nil empty, got %v", out.Classifier)
	}
	if out.MediaType != nil {
		t.Fatalf("absent mediaType should be nil, got %v", out.MediaType)
	}
}

func ptr[T any](v T) *T { return &v }
