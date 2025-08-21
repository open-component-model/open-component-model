package hooks

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	"ocm.software/open-component-model/cli/cmd/setup"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

/*
   ──────────────────────────
   Option interface + builder
   ──────────────────────────
*/

// Option is the single interface all options implement.
type Option interface {
	Apply(b *Builder) error
}

// optionFunc lets simple functions satisfy Option.
type optionFunc func(*Builder) error

func (f optionFunc) Apply(b *Builder) error { return f(b) }

/*
   ──────────────────────────
   Builder (accumulates state)
   ──────────────────────────
*/

type Builder struct {
	cmd *cobra.Command

	// buckets of config the setup layer expects
	fsOpts map[string]setup.SetupFilesystemConfigOption
}

func newBuilder(cmd *cobra.Command) *Builder {
	return &Builder{
		cmd:    cmd,
		fsOpts: make(map[string]setup.SetupFilesystemConfigOption),
	}
}

// helpers for options to set values
func (b *Builder) setFS(key string, opt setup.SetupFilesystemConfigOption) {
	b.fsOpts[key] = opt
}

// export for setup
func (b *Builder) fsAsSlice() []setup.SetupFilesystemConfigOption {
	out := make([]setup.SetupFilesystemConfigOption, 0, len(b.fsOpts))
	for _, v := range b.fsOpts {
		out = append(out, v)
	}
	return out
}

/*
   ──────────────────────────
   Option constructors
   ──────────────────────────
*/

// WithWorkingDirectory configures the working directory for filesystem setup.
func WithWorkingDirectory(value string) Option {
	return optionFunc(func(b *Builder) error {
		b.setFS(ocmcmd.WorkingDirectoryFlag, setup.WithWorkingDirectory(value))
		return nil
	})
}

// WithTempFolder configures the temp folder for filesystem setup.
func WithTempFolder(value string) Option {
	return optionFunc(func(b *Builder) error {
		b.setFS(ocmcmd.TempFolderFlag, setup.WithTempFolder(value))
		return nil
	})
}

/*
   ──────────────────────────
   PreRun entry points
   ──────────────────────────
*/

// PreRunE sets up the command with defaults (no extra options).
func PreRunE(cmd *cobra.Command, _ []string) error {
	return PreRunEWithOptions(cmd, nil)
}

// PreRunEWithOptions applies options, then overrides with CLI flags.
func PreRunEWithOptions(cmd *cobra.Command, _ []string, opts ...Option) error {
	// logger
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not retrieve logger: %w", err)
	}
	slog.SetDefault(logger)

	// base OCM config
	setup.SetupOCMConfig(cmd)

	// build initial config from options
	b := newBuilder(cmd)
	for _, opt := range opts {
		if err := opt.Apply(b); err != nil {
			return fmt.Errorf("apply option: %w", err)
		}
	}

	// read CLI flags (CLI takes precedence)
	// NOTE: if a flag is present & changed, it overrides builder state.
	if flag := cmd.Flags().Lookup(ocmcmd.TempFolderFlag); flag != nil && flag.Changed {
		if v, err := cmd.Flags().GetString(ocmcmd.TempFolderFlag); err == nil && v != "" {
			b.setFS(ocmcmd.TempFolderFlag, setup.WithTempFolder(v))
		} else if err != nil {
			slog.DebugContext(cmd.Context(), "could not read temp folder flag value", slog.String("error", err.Error()))
		}
	}
	if flag := cmd.Flags().Lookup(ocmcmd.WorkingDirectoryFlag); flag != nil && flag.Changed {
		if v, err := cmd.Flags().GetString(ocmcmd.WorkingDirectoryFlag); err == nil && v != "" {
			b.setFS(ocmcmd.WorkingDirectoryFlag, setup.WithWorkingDirectory(v))
		} else if err != nil {
			slog.DebugContext(cmd.Context(), "could not read working directory flag value", slog.String("error", err.Error()))
		}
	}

	// finalize: apply to the underlying systems
	setup.SetupFilesystemConfig(cmd, b.fsAsSlice()...)

	if err := setup.SetupPluginManager(cmd); err != nil {
		return fmt.Errorf("could not setup plugin manager: %w", err)
	}
	if err := setup.SetupCredentialGraph(cmd); err != nil {
		return fmt.Errorf("could not setup credential graph: %w", err)
	}

	ocmctx.Register(cmd)

	// inherit IO from parent if exists
	if parent := cmd.Parent(); parent != nil {
		cmd.SetOut(parent.OutOrStdout())
		cmd.SetErr(parent.ErrOrStderr())
	}

	return nil
}
