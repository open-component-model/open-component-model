package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	tuiPkg "ocm.software/open-component-model/bindings/go/tui"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

// New creates the tui cobra command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactively explore and transfer OCM component versions",
		Long: `Interactively explore and transfer OCM component versions in a terminal UI.

The TUI provides:
  - Component explorer: browse component versions, resources, sources,
    references, and signatures
  - Transfer wizard: step-by-step transfer of component versions between
    repositories with interactive option selection and graph review`,
		Args:              cobra.NoArgs,
		RunE:              runTUI,
		DisableAutoGenTag: true,
	}

	return cmd
}

func runTUI(cmd *cobra.Command, _ []string) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("the tui command requires an interactive terminal")
	}

	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	config := ocmctx.FromContext(cmd.Context()).Configuration()

	tuiCfg := tuiPkg.Config{
		FetcherFactory:   newFetcherFactory(pluginManager, credentialGraph, config),
		TransferExecutor: newTransferExecutor(pluginManager, credentialGraph, config),
	}

	p := tea.NewProgram(tuiPkg.NewApp(tuiCfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}
