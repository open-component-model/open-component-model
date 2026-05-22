package pull

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	skillResourceType = "ai.skill/v1"

	// Default install directories per coding agent.
	claudeSkillDir = ".claude/skills"
	codexSkillDir  = ".agents/skills"

	flagSkillName = "skill"
	flagOutput    = "output"
	flagTarget    = "target"

	targetClaude = "claude"
	targetCodex  = "codex"
	targetAll    = "all"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull <component-ref> [--skill <name>] [--output <path>]",
		Short: "Pull AI skills from an OCM skill catalogue component",
		Long: `Pull one or all AI skills from an OCM component version that packages skills as resources with type ai.skill/v1.

When --skill is given, only that resource is downloaded. Without --skill, all ai.skill/v1 resources are downloaded.

By default skills are installed for Claude Code (--target claude):
  ~/.claude/skills/<skill-name>/SKILL.md

Use --target codex to install for OpenAI Codex CLI instead:
  ~/.agents/skills/<skill-name>/SKILL.md

Use --target all to install for both agents simultaneously.`,
		Example: `  # Pull a single skill into Claude Code (default)
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --skill ocm-guide

  # Pull all skills into OpenAI Codex CLI
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --target codex

  # Pull all skills into both Claude Code and Codex
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --target all

  # Pull a skill to a custom path
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --skill ocm-guide --output /tmp/ocm-guide.md`,
		Args:              cobra.ExactArgs(1),
		RunE:              pullSkill,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(flagSkillName, "", "name of skill resource to pull (pulls all ai.skill/v1 resources when omitted)")
	cmd.Flags().String(flagOutput, "", "output path for the skill file (only valid with --skill)")
	cmd.Flags().String(flagTarget, targetClaude, `coding agent to install skills for: "claude" (~/.claude/skills/), "codex" (~/.agents/skills/), or "all"`)

	return cmd
}

func pullSkill(cmd *cobra.Command, args []string) error {
	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}

	skillName, err := cmd.Flags().GetString(flagSkillName)
	if err != nil {
		return fmt.Errorf("getting skill flag failed: %w", err)
	}

	output, err := cmd.Flags().GetString(flagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	target, err := cmd.Flags().GetString(flagTarget)
	if err != nil {
		return fmt.Errorf("getting target flag failed: %w", err)
	}

	if output != "" && skillName == "" {
		return fmt.Errorf("--output requires --skill to be set")
	}
	if output != "" && target != targetClaude {
		return fmt.Errorf("--output cannot be combined with --target")
	}

	switch target {
	case targetClaude, targetCodex, targetAll:
	default:
		return fmt.Errorf("unknown --target %q: must be %q, %q, or %q", target, targetClaude, targetCodex, targetAll)
	}

	ref, err := compref.Parse(args[0])
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", args[0], err)
	}

	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(cmd.Context(), pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, nil, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(cmd.Context(), ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(cmd.Context(), ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	var candidates []descriptor.Resource
	for _, res := range desc.Component.Resources {
		if res.Type != skillResourceType {
			continue
		}
		if skillName != "" && res.Name != skillName {
			continue
		}
		candidates = append(candidates, res)
	}

	if len(candidates) == 0 {
		if skillName != "" {
			return fmt.Errorf("skill %q not found in component (no resource with type %s and name %q)", skillName, skillResourceType, skillName)
		}
		return fmt.Errorf("no %s resources found in component %s:%s", skillResourceType, ref.Component, ref.Version)
	}

	if output != "" && len(candidates) > 1 {
		return fmt.Errorf("--output requires exactly one matching skill, got %d", len(candidates))
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory failed: %w", err)
	}

	destDirs := installDirs(homeDir, target)

	for _, res := range candidates {
		identity := runtime.Identity{"name": res.Name}

		if output != "" {
			data, err := shared.DownloadResourceData(cmd.Context(), pluginManager, credentialGraph, ref.Component, ref.Version, repo, &res, identity)
			if err != nil {
				return fmt.Errorf("downloading skill %q failed: %w", res.Name, err)
			}
			if err := writeSkillFile(data, output); err != nil {
				return fmt.Errorf("saving skill %q to %q failed: %w", res.Name, output, err)
			}
			logger.Info("skill installed", slog.String("skill", res.Name), slog.String("output", output))
			continue
		}

		if err := validateSkillName(res.Name); err != nil {
			return fmt.Errorf("skill resource %q has invalid name: %w", res.Name, err)
		}

		data, err := shared.DownloadResourceData(cmd.Context(), pluginManager, credentialGraph, ref.Component, ref.Version, repo, &res, identity)
		if err != nil {
			return fmt.Errorf("downloading skill %q failed: %w", res.Name, err)
		}

		// Buffer content so it can be written to multiple targets.
		content, err := readBlobBytes(data)
		if err != nil {
			return fmt.Errorf("reading skill %q content failed: %w", res.Name, err)
		}

		for _, base := range destDirs {
			dest := filepath.Join(base, res.Name, "SKILL.md")
			if !strings.HasPrefix(dest, base+string(filepath.Separator)) {
				return fmt.Errorf("skill resource name %q resolves outside skills directory", res.Name)
			}
			if err := writeBytesToFile(content, dest); err != nil {
				return fmt.Errorf("saving skill %q to %q failed: %w", res.Name, dest, err)
			}
			logger.Info("skill installed", slog.String("skill", res.Name), slog.String("output", dest))
		}
	}

	return nil
}

// installDirs returns the skills base directories for the given target agent.
func installDirs(homeDir, target string) []string {
	switch target {
	case targetCodex:
		return []string{filepath.Join(homeDir, codexSkillDir)}
	case targetAll:
		return []string{
			filepath.Join(homeDir, claudeSkillDir),
			filepath.Join(homeDir, codexSkillDir),
		}
	default: // targetClaude
		return []string{filepath.Join(homeDir, claudeSkillDir)}
	}
}

// validateSkillName rejects names that could escape the skills directory via path traversal.
func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	clean := filepath.Clean(name)
	if clean != name {
		return fmt.Errorf("name %q contains path sequences that would be cleaned", name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("name %q must not be an absolute path", name)
	}
	if strings.Contains(name, string(filepath.Separator)) || strings.Contains(name, "/") {
		return fmt.Errorf("name %q must not contain path separators", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("name %q must not start with a dot", name)
	}
	return nil
}

// writeSkillFile writes a blob stream to dest, truncating any existing file.
// Unlike the shared SaveBlobToFile helper (which appends), skills are always overwritten on pull.
func writeSkillFile(b blob.ReadOnlyBlob, dest string) (err error) {
	content, err := readBlobBytes(b)
	if err != nil {
		return err
	}
	return writeBytesToFile(content, dest)
}

// readBlobBytes drains a read-only blob into a byte slice so the content can be written to multiple destinations.
func readBlobBytes(b blob.ReadOnlyBlob) (_ []byte, err error) {
	r, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("reading blob failed: %w", err)
	}
	defer func() {
		err = errors.Join(err, r.Close())
	}()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return nil, fmt.Errorf("reading blob content failed: %w", err)
	}
	return buf.Bytes(), nil
}

func writeBytesToFile(content []byte, dest string) error {
	if dir := filepath.Dir(dest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %q failed: %w", dir, err)
		}
	}
	if err := os.WriteFile(dest, content, 0o600); err != nil {
		return fmt.Errorf("writing to %q failed: %w", dest, err)
	}
	return nil
}
