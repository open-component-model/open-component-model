package v2alpha1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newMaven() *v2alpha1.Maven {
	return &v2alpha1.Maven{
		RepoURL:    "https://repo1.maven.org/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.2.3",
		Artifacts:  []v2alpha1.Artifact{{Extension: "jar"}},
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, newMaven().Validate())
	})
	t.Run("valid with several artifacts", func(t *testing.T) {
		m := newMaven()
		m.Artifacts = []v2alpha1.Artifact{
			{Extension: "pom"},
			{Extension: "jar"},
			{Extension: "jar", Classifier: "sources"},
		}
		require.NoError(t, m.Validate())
	})
	t.Run("missing groupId", func(t *testing.T) {
		m := newMaven()
		m.GroupID = ""
		require.ErrorContains(t, m.Validate(), "groupId")
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
	t.Run("empty artifacts", func(t *testing.T) {
		m := newMaven()
		m.Artifacts = nil
		require.ErrorContains(t, m.Validate(), "artifacts must list at least one file")
	})
	t.Run("artifact without extension", func(t *testing.T) {
		m := newMaven()
		m.Artifacts = []v2alpha1.Artifact{{Classifier: "sources"}}
		require.ErrorContains(t, m.Validate(), "artifacts[0]: extension is required")
	})
	t.Run("duplicate artifact", func(t *testing.T) {
		m := newMaven()
		m.Artifacts = []v2alpha1.Artifact{{Extension: "jar"}, {Extension: "jar"}}
		require.ErrorContains(t, m.Validate(), "artifacts[1]: duplicate entry")
	})
	t.Run("relative repoUrl", func(t *testing.T) {
		m := newMaven()
		m.RepoURL = "/maven2"
		require.ErrorContains(t, m.Validate(), "absolute URL")
	})
	t.Run("path traversal in a coordinate", func(t *testing.T) {
		for name, mutate := range map[string]func(*v2alpha1.Maven){
			"groupId":    func(m *v2alpha1.Maven) { m.GroupID = "../../secret" },
			"artifactId": func(m *v2alpha1.Maven) { m.ArtifactID = "lib/../../x" },
			"version":    func(m *v2alpha1.Maven) { m.Version = `1.0\..` },
		} {
			m := newMaven()
			mutate(m)
			err := m.Validate()
			require.ErrorContains(t, err, name)
			require.ErrorContains(t, err, "path separators")
		}
	})
	t.Run("path traversal in classifier or extension", func(t *testing.T) {
		for _, a := range []v2alpha1.Artifact{
			{Extension: "../jar"},
			{Extension: "jar", Classifier: "a/b"},
			{Extension: `jar\`},
		} {
			m := newMaven()
			m.Artifacts = []v2alpha1.Artifact{a}
			require.ErrorContains(t, m.Validate(), "path separators")
		}
	})
	t.Run("errors are joined", func(t *testing.T) {
		m := &v2alpha1.Maven{}
		err := m.Validate()
		require.Error(t, err)
		for _, want := range []string{"groupId", "artifactId", "version", "repoUrl", "artifacts"} {
			assert.Contains(t, err.Error(), want)
		}
	})
}

func TestMaven_IsPinnedVersion(t *testing.T) {
	for version, want := range map[string]bool{
		"1.2.3":        true,
		"1.2.3-rc1":    true,
		"1.0-SNAPSHOT": false,
		"LATEST":       false,
		"RELEASE":      false,
	} {
		m := newMaven()
		m.Version = version
		assert.Equal(t, want, m.IsPinnedVersion(), version)
	}
}

func TestMaven_JSONRoundTrip(t *testing.T) {
	in := v2alpha1.Maven{
		Type:       runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version),
		RepoURL:    "https://repo1.maven.org/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.2.3",
		Artifacts: []v2alpha1.Artifact{
			{Extension: "jar"},
			{Extension: "jar", Classifier: "sources"},
		},
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "maven/v2alpha1",
		"repoUrl": "https://repo1.maven.org/maven2",
		"groupId": "com.example",
		"artifactId": "lib",
		"version": "1.2.3",
		"artifacts": [
			{"extension": "jar"},
			{"extension": "jar", "classifier": "sources"}
		]
	}`, string(data))

	var out v2alpha1.Maven
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestMaven_DeepCopyIsolatesArtifacts(t *testing.T) {
	in := newMaven()
	out := in.DeepCopy()
	out.Artifacts[0].Extension = "pom"
	assert.Equal(t, "jar", in.Artifacts[0].Extension)
}
