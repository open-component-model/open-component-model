package v1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
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
