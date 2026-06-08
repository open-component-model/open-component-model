package push_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/cli/cmd/internal/test"
)

func buildSkillsDir(t *testing.T, skills map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range skills {
		skillDir := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(skillDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
	}
	return dir
}

func TestPushPrintsConstructorToStdout(t *testing.T) {
	skillsDir := buildSkillsDir(t, map[string]string{
		"ocm-guide":       "---\nname: ocm-guide\n---\n# OCM Guide\n",
		"golang-patterns": "---\nname: golang-patterns\n---\n# Go\n",
	})

	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("skill", "push", skillsDir, "--component", "myorg.io/skills", "--version", "1.0.0"),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &spec), "stdout must be valid YAML")

	components, ok := spec["components"].([]any)
	require.True(t, ok, "expected 'components' list")
	require.Len(t, components, 1)

	comp := components[0].(map[string]any)
	require.Equal(t, "myorg.io/skills", comp["name"])
	require.Equal(t, "1.0.0", comp["version"])

	resources, ok := comp["resources"].([]any)
	require.True(t, ok)
	require.Len(t, resources, 2)

	names := make([]string, 0, len(resources))
	for _, r := range resources {
		res := r.(map[string]any)
		require.Equal(t, "ai.skill/v1", res["type"])
		names = append(names, res["name"].(string))
	}
	require.ElementsMatch(t, []string{"ocm-guide", "golang-patterns"}, names)
}

func TestPushWritesConstructorToFile(t *testing.T) {
	skillsDir := buildSkillsDir(t, map[string]string{
		"my-skill": "---\nname: my-skill\n---\n# Content\n",
	})

	outputFile := filepath.Join(t.TempDir(), "constructor.yaml")
	out := new(bytes.Buffer)

	_, err := test.OCM(t,
		test.WithArgs("skill", "push", skillsDir,
			"--component", "myorg.io/skills",
			"--version", "2.0.0",
			"--output", outputFile,
		),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)
	require.FileExists(t, outputFile)

	stdout := out.String()
	require.Contains(t, stdout, "Constructor written to")
	require.Contains(t, stdout, "ocm add component-version")

	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(data, &spec))

	components := spec["components"].([]any)
	comp := components[0].(map[string]any)
	require.Equal(t, "2.0.0", comp["version"])
}

func TestPushExtractsProviderFromComponentName(t *testing.T) {
	skillsDir := buildSkillsDir(t, map[string]string{
		"ocm-guide": "# content",
	})

	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("skill", "push", skillsDir, "--component", "acme.corp/skills", "--version", "1.0.0"),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &spec))

	comp := spec["components"].([]any)[0].(map[string]any)
	provider := comp["provider"].(map[string]any)
	require.Equal(t, "acme.corp", provider["name"])
}

func TestPushExplicitProviderOverridesDefault(t *testing.T) {
	skillsDir := buildSkillsDir(t, map[string]string{
		"ocm-guide": "# content",
	})

	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("skill", "push", skillsDir,
			"--component", "acme.corp/skills",
			"--version", "1.0.0",
			"--provider", "my-team",
		),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &spec))

	comp := spec["components"].([]any)[0].(map[string]any)
	provider := comp["provider"].(map[string]any)
	require.Equal(t, "my-team", provider["name"])
}

func TestPushSkipsSubdirsWithoutSkillMD(t *testing.T) {
	dir := t.TempDir()
	// valid skill
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "real-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real-skill", "SKILL.md"), []byte("# content"), 0o644))
	// directory without SKILL.md
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "no-skill-here"), 0o755))
	// plain file at top level — must be ignored
	require.NoError(t, os.WriteFile(filepath.Join(dir, "random.txt"), []byte("ignored"), 0o644))

	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("skill", "push", dir, "--component", "org.io/skills", "--version", "1.0.0"),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &spec))

	resources := spec["components"].([]any)[0].(map[string]any)["resources"].([]any)
	require.Len(t, resources, 1)
	require.Equal(t, "real-skill", resources[0].(map[string]any)["name"])
}

func TestPushEmptySkillsDirFails(t *testing.T) {
	dir := t.TempDir()

	_, err := test.OCM(t,
		test.WithArgs("skill", "push", dir, "--component", "org.io/skills", "--version", "1.0.0"),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no skills found")
}

func TestPushRequiresComponentFlag(t *testing.T) {
	dir := buildSkillsDir(t, map[string]string{"ocm-guide": "# content"})

	_, err := test.OCM(t,
		test.WithArgs("skill", "push", dir),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err)
}

func TestPushResourcePathsAreAbsolute(t *testing.T) {
	skillsDir := buildSkillsDir(t, map[string]string{
		"ocm-guide": "# content",
	})

	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("skill", "push", skillsDir, "--component", "org.io/skills", "--version", "1.0.0"),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &spec))

	resources := spec["components"].([]any)[0].(map[string]any)["resources"].([]any)
	input := resources[0].(map[string]any)["input"].(map[string]any)
	path := input["path"].(string)
	require.True(t, strings.HasPrefix(path, "/"), "resource path must be absolute, got %q", path)
}

func TestPushRoundTrip(t *testing.T) {
	// Full round-trip: push generates constructor, add packages it, pull retrieves the skill.
	// All paths must live under the same root so ocm add can resolve them.
	skillContent := []byte("---\nname: test-skill\ndescription: Round-trip test\n---\n# Test Skill\n")

	root := t.TempDir()

	skillsDir := filepath.Join(root, "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "test-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "test-skill", "SKILL.md"), skillContent, 0o644))

	constructorFile := filepath.Join(root, "constructor.yaml")
	_, err := test.OCM(t,
		test.WithArgs("skill", "push", skillsDir,
			"--component", "org.io/skill-catalogue",
			"--version", "1.0.0",
			"--output", constructorFile,
		),
		test.WithOutput(new(bytes.Buffer)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)
	require.FileExists(t, constructorFile)

	repoPath := filepath.Join(root, "catalogue")
	_, err = test.OCM(t,
		test.WithArgs("add", "component-version",
			"--repository", repoPath,
			"--constructor", constructorFile,
		),
		test.WithOutput(new(bytes.Buffer)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	// use a fresh home so pull goes to a predictable location
	home := t.TempDir()
	t.Setenv("HOME", home)

	ref := "CTF::" + repoPath + "//org.io/skill-catalogue:1.0.0"
	_, err = test.OCM(t,
		test.WithArgs("skill", "pull", ref, "--skill", "test-skill"),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.NoError(t, err)

	installed := filepath.Join(home, ".claude", "skills", "test-skill", "SKILL.md")
	data, err := os.ReadFile(installed)
	require.NoError(t, err)
	require.Equal(t, skillContent, data)
}

func TestPushRejectsSymlinkedSkillFile(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	// Place the real content outside the skills tree and symlink SKILL.md to it.
	outside := filepath.Join(t.TempDir(), "real.md")
	require.NoError(t, os.WriteFile(outside, []byte("# content"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(skillDir, "SKILL.md")))

	_, err := test.OCM(t,
		test.WithArgs("skill", "push", root, "--component", "org.io/skills", "--version", "1.0.0"),
		test.WithOutput(new(bytes.Buffer)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.Error(t, err, "push must reject a SKILL.md that is a symlink")
}
