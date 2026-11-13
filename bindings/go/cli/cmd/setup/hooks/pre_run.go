package hooks

import (
	"crypto/fips140"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/cli/cmd/setup"
	"ocm.software/open-component-model/bindings/go/cli/cmd/version"
	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
)

// PreRunE is the default PreRun hook with no extra options.
func PreRunE(cmd *cobra.Command, _ []string) error {
	return PreRunEWithConfig(cmd, Config{})
}

// Config holds setup values. CLI flags can override these.
type Config struct {
	setup.FilesystemConfigOptions
}

// PreRunEWithConfig applies initial config, then merges CLI flags, then finalizes setup.
func PreRunEWithConfig(cmd *cobra.Command, cfg Config) error {
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("get logger: %w", err)
	}
	slog.SetDefault(logger)
	nonText, err := nonTextLogRenderer(cmd)
	if err != nil {
		return err
	}
	if nonText {
		cmd.SetContext(progress.WithNonTextLogRenderer(cmd.Context()))
	}
	slog.DebugContext(cmd.Context(), "FIPS 140-3 mode",
		slog.Bool("enabled", fips140.Enabled()), slog.String("module", fips140.Version()))

	setup.Syscalls(cmd)
	// Best-effort first-startup auto configuration. Failures must not block command execution.
	if err := setup.AutoConfigure(cmd); err != nil {
		slog.WarnContext(cmd.Context(), "auto configuration failed", slog.String("error", err.Error()))
	}
	if err := setup.OCMConfig(cmd); err != nil {
		return fmt.Errorf("setup ocm config: %w", err)
	}

	// Apply filesystem config
	setup.FilesystemConfig(cmd, cfg.FilesystemConfigOptions)

	// Remaining setup
	if err := setup.PluginManager(cmd); err != nil {
		return fmt.Errorf("setup plugin manager: %w", err)
	}
	if err := setup.CredentialGraph(cmd); err != nil {
		return fmt.Errorf("setup credential graph: %w", err)
	}
	ocmctx.Register(cmd)

	// Inherit output streams from parent command if available.
	if parent := cmd.Parent(); parent != nil {
		cmd.SetOut(parent.OutOrStdout())
		cmd.SetErr(parent.ErrOrStderr())
	}

	setup.VersionCheck(cmd, version.BuildVersion)

	return nil
}

// nonTextLogRenderer reports whether the log records are rendered by something
// other than a plain-text renderer. Whether the output is interactive is not
// part of this decision: the progress package owns the stream it renders on and
// detects that itself. The requested format alone decides, not whether the flag
// was passed, because --logformat text asks for the default renderer and must
// behave like omitting the flag. Only progress rendering is affected, not the
// live render mode of get/add, which animates data on stdout.
func nonTextLogRenderer(cmd *cobra.Command) (bool, error) {
	format, err := enum.Get(cmd.Flags(), log.FormatFlagName)
	if err != nil {
		return false, fmt.Errorf("get log format: %w", err)
	}
	return format != log.FormatText, nil
}
