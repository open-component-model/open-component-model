package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_SkillPushPull(t *testing.T) {
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfgPath := writeOCMConfig(t, registry)

	component := "ocm.software/skill-catalogue"
	version := "1.0.0"

	root := t.TempDir()

	skillContent := []byte("---\nname: test-skill\ndescription: Integration test skill\n---\n\n# Test Skill\n\nContent.\n")
	skillsDir := filepath.Join(root, "skills")
	r.NoError(os.MkdirAll(filepath.Join(skillsDir, "test-skill"), 0o755))
	r.NoError(os.WriteFile(filepath.Join(skillsDir, "test-skill", "SKILL.md"), skillContent, 0o644))

	constructorPath := filepath.Join(root, "constructor.yaml")

	t.Run("push generates constructor", func(t *testing.T) {
		pushCMD := cmd.New()
		pushCMD.SetArgs([]string{
			"skill", "push", skillsDir,
			"--component", component,
			"--version", version,
			"--output", constructorPath,
		})
		require.NoError(t, pushCMD.ExecuteContext(t.Context()))
		require.FileExists(t, constructorPath)
	})

	repoRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	t.Run("add component-version packages skills into OCI registry", func(t *testing.T) {
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add", "component-version",
			"--repository", repoRef,
			"--constructor", constructorPath,
			"--config", cfgPath,
		})
		require.NoError(t, addCMD.ExecuteContext(t.Context()))
	})

	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("pull installs skill from OCI registry", func(t *testing.T) {
		ref := fmt.Sprintf("%s//%s:%s", repoRef, component, version)
		pullCMD := cmd.New()
		pullCMD.SetArgs([]string{
			"skill", "pull", ref,
			"--skill", "test-skill",
			"--config", cfgPath,
		})
		require.NoError(t, pullCMD.ExecuteContext(t.Context()))

		installed := filepath.Join(home, ".claude", "skills", "test-skill", "SKILL.md")
		data, err := os.ReadFile(installed)
		require.NoError(t, err)
		require.Equal(t, skillContent, data)
	})
}

func Test_Integration_SkillPullAll(t *testing.T) {
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfgPath := writeOCMConfig(t, registry)

	component := "ocm.software/multi-skill-catalogue"
	version := "1.0.0"

	skills := map[string][]byte{
		"skill-alpha": []byte("---\nname: skill-alpha\n---\n# Alpha\n"),
		"skill-beta":  []byte("---\nname: skill-beta\n---\n# Beta\n"),
	}

	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	for name, content := range skills {
		r.NoError(os.MkdirAll(filepath.Join(skillsDir, name), 0o755))
		r.NoError(os.WriteFile(filepath.Join(skillsDir, name, "SKILL.md"), content, 0o644))
	}

	constructorPath := filepath.Join(root, "constructor.yaml")
	pushCMD := cmd.New()
	pushCMD.SetArgs([]string{
		"skill", "push", skillsDir,
		"--component", component,
		"--version", version,
		"--output", constructorPath,
	})
	r.NoError(pushCMD.ExecuteContext(t.Context()))

	repoRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", repoRef,
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()))

	home := t.TempDir()
	t.Setenv("HOME", home)

	ref := fmt.Sprintf("%s//%s:%s", repoRef, component, version)
	pullCMD := cmd.New()
	pullCMD.SetArgs([]string{
		"skill", "pull", ref,
		"--config", cfgPath,
	})
	r.NoError(pullCMD.ExecuteContext(t.Context()))

	for name, expected := range skills {
		path := filepath.Join(home, ".claude", "skills", name, "SKILL.md")
		data, err := os.ReadFile(path)
		r.NoError(err, "skill %q not installed", name)
		r.Equal(expected, data, "skill %q content mismatch", name)
	}
}

func Test_Integration_SkillPullIdempotent(t *testing.T) {
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfgPath := writeOCMConfig(t, registry)

	component := "ocm.software/idempotent-skill-catalogue"
	version := "1.0.0"
	skillContent := []byte("---\nname: idem-skill\n---\n# Idem\n")

	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	r.NoError(os.MkdirAll(filepath.Join(skillsDir, "idem-skill"), 0o755))
	r.NoError(os.WriteFile(filepath.Join(skillsDir, "idem-skill", "SKILL.md"), skillContent, 0o644))

	constructorPath := filepath.Join(root, "constructor.yaml")
	pushCMD := cmd.New()
	pushCMD.SetArgs([]string{
		"skill", "push", skillsDir,
		"--component", component,
		"--version", version,
		"--output", constructorPath,
	})
	r.NoError(pushCMD.ExecuteContext(t.Context()))

	repoRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", repoRef,
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()))

	home := t.TempDir()
	t.Setenv("HOME", home)

	ref := fmt.Sprintf("%s//%s:%s", repoRef, component, version)
	for i := range 3 {
		pullCMD := cmd.New()
		pullCMD.SetArgs([]string{
			"skill", "pull", ref,
			"--skill", "idem-skill",
			"--config", cfgPath,
		})
		r.NoError(pullCMD.ExecuteContext(t.Context()), "pull %d failed", i+1)
	}

	installed := filepath.Join(home, ".claude", "skills", "idem-skill", "SKILL.md")
	data, err := os.ReadFile(installed)
	r.NoError(err)
	r.Equal(skillContent, data, "repeated pulls must not duplicate content")
}

func writeOCMConfig(t *testing.T, registry *internal.OCIRegistry) string {
	t.Helper()
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`, registry.Host, registry.Port, registry.User, registry.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))
	return cfgPath
}
