package hooks

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/setup"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
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

	setup.OCMConfig(cmd)

	// Apply filesystem config
	setup.FilesystemConfig(cmd, cfg.FilesystemConfigOptions)

	// Apply HTTP client config
	setup.HTTPConfig(cmd)

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

	return nil
}
