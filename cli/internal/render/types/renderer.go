package types

import (
	"io"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/cli/internal/subsystem"
)

// DocRenderer defines the interface for rendering OCM documentation.
type DocRenderer interface {
	// SetRootCommand sets the root command used for dynamic discovery of linked commands.
	SetRootCommand(cmd *cobra.Command)

	// RenderSubsystem renders the documentation for a subsystem (metadata + guides).
	RenderSubsystem(w io.Writer, s *subsystem.Subsystem) error

	// RenderType renders the documentation for a specific OCM type.
	RenderType(w io.Writer, s *subsystem.Subsystem, typ runtime.Type, schema io.Reader) error
}
