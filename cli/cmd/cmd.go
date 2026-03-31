package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/cli/cmd/add"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/cmd/describe"
	"ocm.software/open-component-model/cli/cmd/download"
	"ocm.software/open-component-model/cli/cmd/generate"
	"ocm.software/open-component-model/cli/cmd/get"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	pluginregistry "ocm.software/open-component-model/cli/cmd/plugins"
	"ocm.software/open-component-model/cli/cmd/setup"
	"ocm.software/open-component-model/cli/cmd/setup/hooks"
	"ocm.software/open-component-model/cli/cmd/sign"
	"ocm.software/open-component-model/cli/cmd/transfer"
	"ocm.software/open-component-model/cli/cmd/verify"
	"ocm.software/open-component-model/cli/cmd/version"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

var pluginDirectoryDefault = filepath.Join("$HOME", ".config", "ocm", "plugins")

// Execute adds all child commands to the Cmd command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the Cmd.
func Execute() {
	root := New()
	// Eagerly discover and inject CLI command plugins so that dynamically-provided
	// subcommands are part of the Cobra tree before Execute() resolves os.Args.
	injectPluginCommandsEarly(root)
	err := root.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// injectPluginCommandsEarly parses --plugin-directory from os.Args without executing
// the full setup chain, discovers plugins, and injects any cliCommand capabilities
// as Cobra subcommands. This must run before root.Execute() so that Cobra can resolve
// plugin-provided commands when traversing os.Args.
func injectPluginCommandsEarly(root *cobra.Command) {
	// Minimal flag set — only what we need to locate plugins.
	fs := pflag.NewFlagSet("early", pflag.ContinueOnError)
	pluginDir := fs.String(ocmcmd.PluginDirectoryFlag, os.ExpandEnv(pluginDirectoryDefault), "")
	// Silence usage output and ignore unknown flags; we only care about --plugin-directory.
	fs.Usage = func() {}
	fs.ParseErrorsAllowlist.UnknownFlags = true
	_ = fs.Parse(os.Args[1:])

	dir := os.ExpandEnv(*pluginDir)
	ctx := context.Background()
	pm := manager.NewPluginManager(ctx)
	if err := pm.RegisterPlugins(ctx, dir, manager.WithIdleTimeout(time.Hour)); err != nil {
		if !errors.Is(err, manager.ErrNoPluginsFound) {
			slog.Debug("early plugin discovery failed", "error", err)
		}
		return
	}

	// Store the manager so the full PreRunE setup can reuse it rather than re-registering.
	ctx, err := ocmctx.WithPluginManager(ctx, pm)
	if err != nil {
		slog.Debug("could not store plugin manager in early context", "error", err)
		return
	}
	root.SetContext(ctx)

	if err := setup.InjectCLICommandPlugins(root); err != nil {
		slog.Debug("early CLI command plugin injection failed", "error", err)
	}
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ocm [sub-command]",
		Short: "The official Open Component Model (OCM) CLI",
		Long: `The Open Component Model command line client supports the work with OCM
  artifacts, like Component Archives, Common Transport Archive,
  Component Repositories, and Component Versions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: hooks.PreRunE,
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	configuration.RegisterConfigFlag(cmd)

	cmd.PersistentFlags().String(ocmcmd.TempFolderFlag, "", `Specify a custom temporary folder path for filesystem operations.`)
	cmd.PersistentFlags().Duration(ocmcmd.PluginShutdownTimeoutFlag, ocmcmd.PluginShutdownTimeoutDefault,
		`Timeout for plugin shutdown. If a plugin does not shut down within this time, it is forcefully killed`)
	cmd.PersistentFlags().String(ocmcmd.PluginDirectoryFlag, pluginDirectoryDefault, `default directory path for ocm plugins.`)
	cmd.PersistentFlags().String(ocmcmd.WorkingDirectoryFlag, "", `Specify a custom working directory path to load resources from.`)
	log.RegisterLoggingFlags(cmd.PersistentFlags())
	cmd.AddCommand(generate.New())
	cmd.AddCommand(get.New())
	cmd.AddCommand(add.New())
	cmd.AddCommand(version.New())
	cmd.AddCommand(download.New())
	cmd.AddCommand(verify.New())
	cmd.AddCommand(sign.New())
	cmd.AddCommand(pluginregistry.New())
	cmd.AddCommand(transfer.New())
	cmd.AddCommand(describe.New())
	return cmd
}
