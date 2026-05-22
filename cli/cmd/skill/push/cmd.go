package push

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const (
	flagComponent  = "component"
	flagVersion    = "version"
	flagProvider   = "provider"
	flagRepository = "repository"
	flagOutputFile = "output"

	skillResourceType = "ai.skill/v1"
	skillFileName     = "SKILL.md"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push <skills-dir> --component <name> --version <v>",
		Short: "Generate an OCM component-constructor for a directory of AI skills",
		Long: `Generate a component-constructor.yaml that packages all SKILL.md files found
in <skills-dir> as ai.skill/v1 resources inside a single OCM component.

Prints the constructor YAML to stdout by default. Use --output to write to a file.

After generation, run:
  ocm add component-version --repository <ref> --constructor <output-file>`,
		Example: `  # Print constructor to stdout
  ocm skill push ~/.claude/skills --component jakob.io/ai-skill-catalogue --version 1.0.0

  # Write constructor to file
  ocm skill push ~/.claude/skills --component jakob.io/ai-skill-catalogue --version 1.0.0 --output constructor.yaml
  ocm add component-version --repository ./catalogue --constructor constructor.yaml`,
		Args:              cobra.ExactArgs(1),
		RunE:              pushSkills,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(flagComponent, "", "component name (required, e.g. jakob.io/ai-skill-catalogue)")
	cmd.Flags().String(flagVersion, "1.0.0", "component version")
	cmd.Flags().String(flagProvider, "", "provider name (defaults to the domain part of the component name)")
	cmd.Flags().String(flagRepository, "transport-archive", "target repository reference (printed in usage hint)")
	cmd.Flags().String(flagOutputFile, "", "write constructor YAML to this file instead of stdout")

	_ = cmd.MarkFlagRequired(flagComponent)

	return cmd
}

type constructorResource struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Version string    `json:"version"`
	Input   fileInput `json:"input"`
}

type fileInput struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type constructorComponent struct {
	Name      string                `json:"name"`
	Version   string                `json:"version"`
	Provider  map[string]string     `json:"provider"`
	Resources []constructorResource `json:"resources"`
}

type constructorSpec struct {
	Components []constructorComponent `json:"components"`
}

func pushSkills(cmd *cobra.Command, args []string) error {
	skillsDir := args[0]

	component, err := cmd.Flags().GetString(flagComponent)
	if err != nil {
		return fmt.Errorf("getting component flag failed: %w", err)
	}

	version, err := cmd.Flags().GetString(flagVersion)
	if err != nil {
		return fmt.Errorf("getting version flag failed: %w", err)
	}

	provider, err := cmd.Flags().GetString(flagProvider)
	if err != nil {
		return fmt.Errorf("getting provider flag failed: %w", err)
	}

	outputFile, err := cmd.Flags().GetString(flagOutputFile)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	repo, err := cmd.Flags().GetString(flagRepository)
	if err != nil {
		return fmt.Errorf("getting repository flag failed: %w", err)
	}

	if provider == "" {
		provider = extractProvider(component)
	}

	absSkillsDir, err := filepath.Abs(skillsDir)
	if err != nil {
		return fmt.Errorf("resolving skills directory failed: %w", err)
	}

	// resolve symlinks on the root to ensure we work with the real path
	absSkillsDir, err = filepath.EvalSymlinks(absSkillsDir)
	if err != nil {
		return fmt.Errorf("evaluating symlinks for skills directory %q failed: %w", absSkillsDir, err)
	}

	entries, err := os.ReadDir(absSkillsDir)
	if err != nil {
		return fmt.Errorf("reading skills directory %q failed: %w", absSkillsDir, err)
	}

	var resources []constructorResource
	for _, entry := range entries {
		// reject symlinked directories to prevent packaging files outside skillsDir
		info, err := os.Lstat(filepath.Join(absSkillsDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("stat of skill directory %q failed: %w", entry.Name(), err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		skillPath := filepath.Join(absSkillsDir, entry.Name(), skillFileName)
		if _, err := os.Stat(skillPath); err != nil {
			continue
		}

		resources = append(resources, constructorResource{
			Name:    entry.Name(),
			Type:    skillResourceType,
			Version: version,
			Input: fileInput{
				Type: "file",
				Path: skillPath,
			},
		})
	}

	if len(resources) == 0 {
		return fmt.Errorf("no skills found in %q (expected subdirectories containing %s)", absSkillsDir, skillFileName)
	}

	spec := constructorSpec{
		Components: []constructorComponent{
			{
				Name:      component,
				Version:   version,
				Provider:  map[string]string{"name": provider},
				Resources: resources,
			},
		},
	}

	out, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshalling constructor spec failed: %w", err)
	}

	if outputFile == "" {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), string(out)); err != nil {
			return fmt.Errorf("writing to stdout failed: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(outputFile, out, 0o600); err != nil {
		return fmt.Errorf("writing constructor file %q failed: %w", outputFile, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Constructor written to %s\n", outputFile)
	fmt.Fprintf(cmd.OutOrStdout(), "Run: ocm add component-version --repository %s --constructor %s\n", repo, outputFile)
	return nil
}

// extractProvider returns the domain portion of an OCM component name (before the first '/').
// For example, "jakob.io/ai-skill-catalogue" → "jakob.io".
// Falls back to the full component name if no '/' is present.
func extractProvider(component string) string {
	if idx := strings.Index(component, "/"); idx > 0 {
		return component[:idx]
	}
	return component
}
