package sbom_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/cli/internal/sbom"
)

func document(name string) []byte {
	return []byte(`{"spdxVersion":"SPDX-2.3","name":"` + name + `"}`)
}

// names lists what Write put in dir.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

func TestWrite(t *testing.T) {
	t.Run("writes one file per document, byte for byte", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "out")
		sboms := []repository.SBOM{
			{Name: "sbom", PredicateType: repository.PredicateTypeSPDX, Data: document("a")},
			{Name: "sbom-build", PredicateType: repository.PredicateTypeSPDX, Data: document("b")},
		}

		written, err := sbom.Write(sboms, dir)
		require.NoError(t, err)

		assert.Equal(t, []string{
			filepath.Join(dir, "sbom.spdx.json"),
			filepath.Join(dir, "sbom-build.spdx.json"),
		}, written, "the paths come back in the order the documents were given")

		content, err := os.ReadFile(written[0])
		require.NoError(t, err)
		assert.Equal(t, document("a"), content)
	})

	t.Run("creates the directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "does", "not", "exist")
		_, err := sbom.Write([]repository.SBOM{{Name: "sbom", Data: document("a")}}, dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"sbom.json"}, names(t, dir))
	})

	t.Run("names the file after the predicate type", func(t *testing.T) {
		dir := t.TempDir()
		_, err := sbom.Write([]repository.SBOM{
			{Name: "spdx", PredicateType: repository.PredicateTypeSPDX, Data: document("a")},
			{Name: "cdx", PredicateType: repository.PredicateTypeCycloneDX, Data: document("b")},
			{Name: "unknown", PredicateType: "https://slsa.dev/provenance/v1", Data: document("c")},
		}, dir)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"spdx.spdx.json", "cdx.cdx.json", "unknown.json"}, names(t, dir))
	})

	t.Run("distinguishes documents by platform", func(t *testing.T) {
		dir := t.TempDir()
		_, err := sbom.Write([]repository.SBOM{
			{Name: "sbom", Platform: repository.Platform{OS: "linux", Architecture: "amd64"}, Data: document("a")},
			{Name: "sbom", Platform: repository.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}, Data: document("b")},
		}, dir)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"sbom_linux_amd64.json", "sbom_linux_arm_v7.json"}, names(t, dir))
	})

	t.Run("falls back to the id, then to a placeholder", func(t *testing.T) {
		dir := t.TempDir()
		_, err := sbom.Write([]repository.SBOM{
			{ID: "sha256:abc", Data: document("a")},
			{Data: document("b")},
		}, dir)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"sha256_abc.json", "sbom.json"}, names(t, dir))
	})

	t.Run("does not let one document overwrite another", func(t *testing.T) {
		dir := t.TempDir()
		// Same name, same platform: BuildKit does this for multi stage builds, and a
		// silent overwrite would drop a document we told the user we found.
		written, err := sbom.Write([]repository.SBOM{
			{Name: "sbom", Data: document("a")},
			{Name: "sbom", Data: document("b")},
			{Name: "sbom", Data: document("c")},
		}, dir)
		require.NoError(t, err)
		require.Len(t, written, 3)
		assert.ElementsMatch(t, []string{"sbom.json", "sbom-2.json", "sbom-3.json"}, names(t, dir))
	})

	t.Run("a document cannot escape the output directory", func(t *testing.T) {
		// Name and platform both come out of the artifact, so both are attacker
		// controlled. Nothing may leave dir, and nothing may hide itself.
		dir := filepath.Join(t.TempDir(), "out")
		for _, hostile := range []string{
			"../../etc/passwd",
			"..",
			"/etc/passwd",
			".hidden",
			"a/b/c",
		} {
			_, err := sbom.Write([]repository.SBOM{{Name: hostile, Data: document("a")}}, dir)
			require.NoError(t, err, hostile)
		}

		for _, name := range names(t, dir) {
			assert.NotContains(t, name, "/", "no separator survives")
			assert.False(t, filepath.IsAbs(name), "no absolute path survives")
			assert.NotEqual(t, "..", name)
			assert.NotContains(t, name, "..", "no traversal survives")
			assert.NotEqual(t, ".", string(name[0]), "no hidden file")
		}

		entries, err := os.ReadDir(filepath.Dir(dir))
		require.NoError(t, err)
		require.Len(t, entries, 1, "nothing was written next to the output directory")
	})

	t.Run("rejects an empty set", func(t *testing.T) {
		_, err := sbom.Write(nil, t.TempDir())
		require.Error(t, err)
	})
}

func TestDirectory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		want     string
	}{
		{
			name:     "a name alone reads as a plain directory",
			identity: runtime.Identity{"name": "image"},
			want:     "image",
		},
		{
			name:     "the name leads, the rest follows sorted",
			identity: runtime.Identity{"os": "linux", "architecture": "amd64", "name": "image"},
			want:     "image-amd64-linux",
		},
		{
			name:     "values that are not file name characters fold",
			identity: runtime.Identity{"name": "org/image", "version": "1.0.0"},
			want:     "org_image-1_0_0",
		},
		{
			name:     "an identity without a name stays sorted by key",
			identity: runtime.Identity{"os": "linux", "architecture": "amd64"},
			want:     "amd64-linux",
		},
		{
			name:     "an empty identity falls back",
			identity: runtime.Identity{},
			want:     "sboms",
		},
		{
			name:     "values that fold away entirely fall back",
			identity: runtime.Identity{"name": "..."},
			want:     "sboms",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sbom.Directory(tc.identity))
		})
	}
}
