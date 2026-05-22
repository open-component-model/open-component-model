package pull_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

const (
	testComponent = "jakob.io/ai-skill-catalogue"
	testVersion   = "1.0.0"
)

var testSkillContent = []byte("---\nname: ocm-guide\ndescription: Test skill\n---\n\n# OCM Guide\n\nTest content.\n")

func setupCatalogueWithSkills(t *testing.T, skills map[string][]byte) string {
	t.Helper()
	r := require.New(t)

	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    testComponent,
					Version: testVersion,
				},
			},
			Provider: descriptor.Provider{Name: "jakob.io"},
		},
	}

	ctx := t.Context()
	r.NoError(repo.AddComponentVersion(ctx, desc))

	for name, content := range skills {
		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: testVersion,
				},
			},
			Type:     "ai.skill/v1",
			Relation: descriptor.LocalRelation,
			Access:   &v2.LocalBlob{MediaType: "text/markdown"},
		}
		updated, err := repo.AddLocalResource(ctx, testComponent, testVersion, res, inmemory.New(bytes.NewReader(content)))
		r.NoError(err)

		desc.Component.Resources = append(desc.Component.Resources, *updated)
	}

	r.NoError(repo.AddComponentVersion(ctx, desc))
	return archivePath
}

func refString(archivePath string) string {
	return (&compref.Ref{
		Repository: &ctfv1.Repository{FilePath: archivePath},
		Component:  testComponent,
		Version:    testVersion,
	}).String()
}

func TestPullSingleSkillToOutputPath(t *testing.T) {
	archivePath := setupCatalogueWithSkills(t, map[string][]byte{
		"ocm-guide": testSkillContent,
	})

	outputPath := filepath.Join(t.TempDir(), "SKILL.md")
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath), "--skill", "ocm-guide", "--output", outputPath),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Equal(t, testSkillContent, data)

	entries, err := logs.List()
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	var found bool
	for _, e := range entries {
		if e.Msg == "skill installed" {
			require.Equal(t, "ocm-guide", e.Extras["skill"])
			found = true
			break
		}
	}
	require.True(t, found, "expected 'skill installed' log entry")
}

func TestPullSingleSkillToDefaultLocation(t *testing.T) {
	archivePath := setupCatalogueWithSkills(t, map[string][]byte{
		"my-skill": testSkillContent,
	})

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath), "--skill", "my-skill"),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	expected := filepath.Join(home, ".claude", "skills", "my-skill", "SKILL.md")
	data, err := os.ReadFile(expected)
	require.NoError(t, err)
	require.Equal(t, testSkillContent, data)
}

func TestPullAllSkills(t *testing.T) {
	skillA := []byte("---\nname: skill-a\n---\n# A\n")
	skillB := []byte("---\nname: skill-b\n---\n# B\n")

	archivePath := setupCatalogueWithSkills(t, map[string][]byte{
		"skill-a": skillA,
		"skill-b": skillB,
	})

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	for name, expected := range map[string][]byte{"skill-a": skillA, "skill-b": skillB} {
		path := filepath.Join(home, ".claude", "skills", name, "SKILL.md")
		data, err := os.ReadFile(path)
		require.NoError(t, err, "skill %q not found", name)
		require.Equal(t, expected, data, "skill %q content mismatch", name)
	}
}

func TestPullIsIdempotent(t *testing.T) {
	archivePath := setupCatalogueWithSkills(t, map[string][]byte{
		"ocm-guide": testSkillContent,
	})

	outputPath := filepath.Join(t.TempDir(), "SKILL.md")

	for i := range 3 {
		_, err := test.OCM(t,
			test.WithArgs("skill", "pull", refString(archivePath), "--skill", "ocm-guide", "--output", outputPath),
			test.WithErrorOutput(test.NewJSONLogReader()),
		)
		require.NoError(t, err, "pull %d failed", i+1)
	}

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Equal(t, testSkillContent, data, "repeated pulls must not duplicate content")
}

func TestPullSkillNotFound(t *testing.T) {
	archivePath := setupCatalogueWithSkills(t, map[string][]byte{
		"ocm-guide": testSkillContent,
	})

	_, err := test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath), "--skill", "does-not-exist"),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does-not-exist")
}

func TestPullNoSkillsInComponent(t *testing.T) {
	// component with no ai.skill/v1 resources
	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: testComponent, Version: testVersion},
			},
			Provider: descriptor.Provider{Name: "jakob.io"},
		},
	}
	require.NoError(t, repo.AddComponentVersion(t.Context(), desc))

	_, err = test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ai.skill/v1")
}

func TestPullOutputWithoutSkillFlagFails(t *testing.T) {
	archivePath := setupCatalogueWithSkills(t, map[string][]byte{
		"ocm-guide": testSkillContent,
	})

	_, err := test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath), "--output", "/tmp/something.md"),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--output requires --skill")
}

func TestPullRejectsPathTraversalInResourceName(t *testing.T) {
	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: testComponent, Version: testVersion},
			},
			Provider: descriptor.Provider{Name: "jakob.io"},
		},
	}
	require.NoError(t, repo.AddComponentVersion(t.Context(), desc))

	// Add a resource with a path-traversal name; OCM accepts arbitrary resource names,
	// but pull must reject them when writing to the default skills location.
	malicious := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "../evil", Version: testVersion},
		},
		Type:     "ai.skill/v1",
		Relation: descriptor.LocalRelation,
		Access:   &v2.LocalBlob{MediaType: "text/markdown"},
	}
	updated, err := repo.AddLocalResource(t.Context(), testComponent, testVersion, malicious, inmemory.New(bytes.NewReader(testSkillContent)))
	require.NoError(t, err)
	desc.Component.Resources = append(desc.Component.Resources, *updated)
	require.NoError(t, repo.AddComponentVersion(t.Context(), desc))

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err = test.OCM(t,
		test.WithArgs("skill", "pull", refString(archivePath)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err)
}
