// Package ocminit implements the `ocm init` command: it inspects a local
// repository and scaffolds a component-constructor.yaml for `ocm add cv`. The
// package is named ocminit rather than init to avoid the Go init keyword.
//
// By default classification is fully deterministic (detected manifests + git
// signals): zero configuration, offline, reproducible, no API key. The optional
// --ai-assist flag routes ambiguous decisions through the Jev decisions model
// when a key is configured, gracefully falling back to the deterministic rules
// when it is not.
package ocminit

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	slogctx "github.com/veqryn/slog-context"
	"sigs.k8s.io/yaml"

	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	logflag "ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
	"ocm.software/open-component-model/bindings/go/cli/internal/jev"
	jevconfig "ocm.software/open-component-model/bindings/go/cli/internal/jev/config"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	FlagOutput        = "output"
	FlagDryRun        = "dry-run"
	FlagAIAssist      = "ai-assist"
	FlagMinConfidence = "min-confidence"
	FlagOffline       = "offline"

	DefaultOutput = "component-constructor.yaml"
)

// New constructs the `ocm init` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init [path]",
		Aliases: []string{"scaffold", "discover"},
		Short:   "Scaffold an OCM component-constructor file by inspecting a local repository",
		Long: `Inspect a local repository and write a component-constructor.yaml scaffold you
can build with "ocm add cv".

By default classification is fully deterministic and needs no configuration: it
reads detected manifests (go.mod, Chart.yaml, Dockerfile, package.json, ...) and
git signals (origin, tags, branch) to decide the component kind, version, and
source. It works offline and produces the same result every time.

For a monorepo with several independent deliverables it emits one component per
sub-deliverable plus a root aggregate that references them. A single component
that merely keeps build scaffolding or tooling in subdirectories stays one
component.

Pass --ai-assist to route genuinely ambiguous repositories through the TypeSafe
Jev decisions model (configured via init.cli.config.ocm.software/v1alpha1). If no
API key is available it transparently falls back to the deterministic rules, so
the command never blocks on a missing key.

The command only WRITES the file; run "ocm add cv --constructor <file>" to build
the component version.`,
		Args:              cobra.MaximumNArgs(1),
		DisableAutoGenTag: true,
		RunE:              runInit,
	}
	cmd.Flags().StringP(FlagOutput, "o", DefaultOutput, "path to write the component-constructor file to")
	cmd.Flags().Bool(FlagDryRun, false, "print the constructor YAML to stdout instead of writing a file")
	cmd.Flags().Bool(FlagAIAssist, false, "use the Jev decisions model to refine ambiguous classifications (falls back to deterministic rules if no API key is configured)")
	cmd.Flags().Float64(FlagMinConfidence, 0.75, "with --ai-assist, minimum model confidence to act on a decision; below this the deterministic result is kept")
	cmd.Flags().Bool(FlagOffline, false, "skip the GitHub Releases network call; resolve versions from git tags only")
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Attach the CLI logger to the context so the classification pipeline can
	// emit structured, level-controlled logs of what it discovers
	// (--loglevel debug surfaces every decision). Falls back to the default
	// logger when logging flags are unavailable.
	if logger, lerr := logflag.GetBaseLogger(cmd); lerr == nil && logger != nil {
		ctx = slogctx.NewCtx(ctx, logger)
	}

	root := "."
	if len(args) == 1 {
		root = args[0]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving repository path %q: %w", root, err)
	}
	if info, err := os.Stat(absRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("repository path %q is not a directory", absRoot)
	}

	ocmCtx := ocmctx.FromContext(ctx)
	if ocmCtx == nil {
		return fmt.Errorf("no OCM context found")
	}
	cfg := ocmCtx.Configuration()

	offline, err := cmd.Flags().GetBool(FlagOffline)
	if err != nil {
		return err
	}
	aiAssist, err := cmd.Flags().GetBool(FlagAIAssist)
	if err != nil {
		return err
	}

	// Build the HTTP client from the OCM http.config.ocm.software configuration so
	// GitHub Releases lookups (and any Jev call) honour the same HTTP policy
	// (retries, timeouts, per-host TLS) as the rest of the CLI.
	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(cfg)
	if err != nil {
		return fmt.Errorf("resolving HTTP configuration: %w", err)
	}
	httpClient := ocmhttp.New(ocmhttp.WithConfig(httpCfg))

	stderr := cmd.ErrOrStderr()

	// Default classifier: deterministic rules. Zero config, offline, reproducible.
	classify := func(_ context.Context, state *jev.RepoState) (*jev.Decision, error) {
		return jev.ClassifyRules(state), nil
	}
	classifierNote := "deterministic rules (no model)"

	if aiAssist {
		apiKey := resolveAPIKey(cmd, cfg)
		if apiKey == "" {
			_, _ = fmt.Fprintln(stderr, "note: --ai-assist requested but no Jev API key found; using deterministic rules instead.")
			_, _ = fmt.Fprintf(stderr, "      to enable AI assist, configure a credential for %q or set TYPESAFE_API_KEY / OPENROUTER_API_KEY.\n", credentialHostname(cfg))
		} else {
			jevCfg, err := jevconfig.LookupConfig(cfg)
			if err != nil {
				return fmt.Errorf("loading init configuration: %w", err)
			}
			minConfidence, err := cmd.Flags().GetFloat64(FlagMinConfidence)
			if err != nil {
				return err
			}
			client := jev.NewClient(jevCfg.Endpoint, jevCfg.Model, apiKey, httpClient)
			classify = func(ctx context.Context, state *jev.RepoState) (*jev.Decision, error) {
				return jev.Classify(ctx, client, state, minConfidence)
			}
			classifierNote = fmt.Sprintf("Jev model %q (deterministic rules as fallback)", jevCfg.Model)
		}
	}

	cc, report, err := jev.BuildConstructorTree(ctx, classify, absRoot, offline, httpClient)
	if err != nil {
		return fmt.Errorf("classifying repository: %w", err)
	}

	out, err := yaml.Marshal(cc)
	if err != nil {
		return fmt.Errorf("marshaling constructor: %w", err)
	}

	dryRun, err := cmd.Flags().GetBool(FlagDryRun)
	if err != nil {
		return err
	}

	outputPath, err := cmd.Flags().GetString(FlagOutput)
	if err != nil {
		return err
	}

	if dryRun {
		if _, err := cmd.OutOrStdout().Write(out); err != nil {
			return fmt.Errorf("writing constructor to stdout: %w", err)
		}
	} else {
		if _, statErr := os.Stat(outputPath); statErr == nil {
			_, _ = fmt.Fprintf(stderr, "note: %s already exists; overwriting.\n", outputPath)
		}
		if err := os.WriteFile(outputPath, out, 0o600); err != nil {
			return fmt.Errorf("writing constructor to %q: %w", outputPath, err)
		}
	}

	printReport(cmd, report, classifierNote, dryRun, outputPath, absRoot)
	return nil
}

// credentialHostname returns the configured Jev credential hostname (for the
// no-key hint), defaulting to the package default when config lookup fails.
func credentialHostname(cfg *genericv1.Config) string {
	jevCfg, err := jevconfig.LookupConfig(cfg)
	if err != nil || jevCfg == nil {
		return jevconfig.DefaultCredentialHostname
	}
	return jevCfg.CredentialHostname
}

// resolveAPIKey resolves the Jev API key from the OCM credential graph for the
// configured hostname, falling back to environment variables. Returns "" when no
// key is available.
func resolveAPIKey(cmd *cobra.Command, cfg *genericv1.Config) string {
	ctx := cmd.Context()
	jevCfg, err := jevconfig.LookupConfig(cfg)
	if err == nil && jevCfg != nil {
		if ocmCtx := ocmctx.FromContext(ctx); ocmCtx != nil {
			if graph := ocmCtx.CredentialGraph(); graph != nil {
				identity := runtime.Identity{runtime.IdentityAttributeHostname: jevCfg.CredentialHostname}
				if creds, rerr := graph.Resolve(ctx, identity); rerr == nil {
					if dc, ok := creds.(*credv1.DirectCredentials); ok {
						if v := dc.Properties["apiKey"]; v != "" {
							return v
						}
						if v := dc.Properties["token"]; v != "" {
							return v
						}
					}
				}
			}
		}
	}
	if v := os.Getenv("TYPESAFE_API_KEY"); v != "" {
		return v
	}
	return os.Getenv("OPENROUTER_API_KEY")
}

// printReport writes a human-facing summary: what was discovered, any fields to
// verify, and the exact next command to run.
func printReport(cmd *cobra.Command, report *jev.Report, classifierNote string, dryRun bool, outputPath, absRoot string) {
	var b strings.Builder

	if report.Monorepo {
		fmt.Fprintf(&b, "\nDiscovered a monorepo (%d components) using %s:\n", len(report.Components), classifierNote)
	} else {
		fmt.Fprintf(&b, "\nDiscovered a single component using %s:\n", classifierNote)
	}

	var todos []string
	for _, c := range report.Components {
		fmt.Fprintf(&b, "  • %s@%s\n", c.Name, c.Version)
		detail := fmt.Sprintf("kind: %s", c.Kind)
		if c.VersionSource != "" {
			detail += fmt.Sprintf(" · version from %s", c.VersionSource)
		}
		if c.Provider != "" {
			detail += fmt.Sprintf(" · provider %s", c.Provider)
		}
		fmt.Fprintf(&b, "      %s\n", detail)
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "      – %s\n", n)
		}
		for _, todo := range c.TODOs {
			todos = append(todos, fmt.Sprintf("%s: %s", c.Name, todo))
		}
	}

	if len(todos) > 0 {
		b.WriteString("\nBefore building, verify:\n")
		for _, t := range todos {
			fmt.Fprintf(&b, "  ! %s\n", t)
		}
	}

	b.WriteString("\nNext step:\n")
	if dryRun {
		fmt.Fprintf(&b, "  ocm init %s -o %s   # write the scaffold\n", relOrDot(absRoot), DefaultOutput)
	} else {
		fmt.Fprintf(&b, "  ocm add cv --repository <ctf-or-registry> --constructor %s --working-directory %s\n", outputPath, relOrDot(absRoot))
	}

	_, _ = io.WriteString(cmd.ErrOrStderr(), b.String())
}

// relOrDot returns a path relative to the current working directory, or "." when
// it is the working directory, for a friendlier printed command.
func relOrDot(absRoot string) string {
	wd, err := os.Getwd()
	if err != nil {
		return absRoot
	}
	rel, err := filepath.Rel(wd, absRoot)
	if err != nil || rel == "" {
		return absRoot
	}
	return rel
}
