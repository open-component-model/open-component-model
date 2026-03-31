package setup

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	clicommandv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/clicommand/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

// InjectCLICommandPlugins reads CLI command plugins from the plugin manager
// and adds them as Cobra subcommands to root.
// It must be called after PluginManager and CredentialGraph are set up.
func InjectCLICommandPlugins(root *cobra.Command) error {
	octx := ocmctx.FromContext(root.Context())
	if octx == nil {
		return nil
	}
	pm := octx.PluginManager()
	if pm == nil {
		return nil
	}

	for _, cmdSpec := range pm.CLICommandRegistry.ListCommands() {
		spec := cmdSpec // capture loop variable

		verbCmd := findOrCreateVerbCommand(root, spec.Verb)

		// Skip if already registered (idempotent — safe to call multiple times).
		if verbCmd.HasSubCommands() {
			var found bool
			for _, sub := range verbCmd.Commands() {
				if sub.Use == spec.ObjectType {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}

		pluginCmd := &cobra.Command{
			Use:   spec.ObjectType,
			Short: spec.Short,
			Long:  spec.Long,
			RunE: func(cmd *cobra.Command, args []string) error {
				innerOctx := ocmctx.FromContext(cmd.Context())

				contract, err := pm.CLICommandRegistry.GetPlugin(cmd.Context(), spec.Verb, spec.ObjectType)
				if err != nil {
					return fmt.Errorf("starting CLI command plugin %s %s: %w", spec.Verb, spec.ObjectType, err)
				}

				flags := collectFlagValues(cmd.Flags(), spec.Flags)

				// Resolve credentials via the credential graph.
				var creds map[string]string
				if graph := innerOctx.CredentialGraph(); graph != nil {
					identResp, err := contract.GetCLICommandCredentialConsumerIdentity(
						cmd.Context(),
						&clicommandv1.GetCredentialConsumerIdentityRequest{
							Verb:       spec.Verb,
							ObjectType: spec.ObjectType,
							Flags:      flags,
						},
					)
					if err != nil {
						slog.DebugContext(cmd.Context(), "could not get credential consumer identity from plugin",
							"verb", spec.Verb, "objectType", spec.ObjectType, "error", err)
					} else if len(identResp.Identity) > 0 {
						resolved, resolveErr := graph.Resolve(cmd.Context(), identResp.Identity)
						if resolveErr != nil {
							if !errors.Is(resolveErr, credentials.ErrNotFound) {
								return fmt.Errorf("resolving credentials for %s %s: %w",
									spec.Verb, spec.ObjectType, resolveErr)
							}
							slog.DebugContext(cmd.Context(), "no credentials found for CLI command plugin",
								"verb", spec.Verb, "objectType", spec.ObjectType)
						} else {
							creds = resolved
						}
					}
				}

				resp, err := contract.Execute(cmd.Context(), &clicommandv1.ExecuteRequest{
					Verb:       spec.Verb,
					ObjectType: spec.ObjectType,
					Args:       args,
					Flags:      flags,
				}, creds)
				if err != nil {
					return err
				}
				if resp.Output != "" {
					fmt.Fprint(cmd.OutOrStdout(), resp.Output)
				}
				if resp.ExitCode != 0 {
					os.Exit(resp.ExitCode)
				}
				return nil
			},
		}

		// Register declared flags so Cobra parses them properly.
		for _, f := range spec.Flags {
			registerFlagSpec(pluginCmd, f)
		}

		verbCmd.AddCommand(pluginCmd)
	}

	return nil
}

// findOrCreateVerbCommand finds an existing top-level command with the given
// Use name, or creates a group command and adds it to root.
func findOrCreateVerbCommand(root *cobra.Command, verb string) *cobra.Command {
	for _, sub := range root.Commands() {
		if sub.Use == verb {
			return sub
		}
	}
	cmd := &cobra.Command{
		Use:   verb,
		Short: fmt.Sprintf("%s objects", verb),
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.AddCommand(cmd)
	return cmd
}

// collectFlagValues reads the current value of each declared flag as a string.
func collectFlagValues(fs *pflag.FlagSet, specs []clicommandv1.FlagSpec) map[string]string {
	out := make(map[string]string, len(specs))
	for _, s := range specs {
		if v, err := fs.GetString(s.Name); err == nil {
			out[s.Name] = v
		}
	}
	return out
}

// registerFlagSpec registers a single FlagSpec onto a Cobra command.
func registerFlagSpec(cmd *cobra.Command, f clicommandv1.FlagSpec) {
	shorthand := f.Shorthand
	switch f.Type {
	case "bool":
		cmd.Flags().BoolP(f.Name, shorthand, f.DefaultValue == "true", f.Usage)
	case "int":
		cmd.Flags().IntP(f.Name, shorthand, 0, f.Usage)
	case "stringSlice":
		cmd.Flags().StringSliceP(f.Name, shorthand, nil, f.Usage)
	default: // "string" and anything else
		cmd.Flags().StringP(f.Name, shorthand, f.DefaultValue, f.Usage)
	}
}
