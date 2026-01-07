package types

import (
	"io"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

func NewSchemaRenderer() *SchemaRenderer {
	return &SchemaRenderer{}
}

// SchemaRenderer renders documentation for types directly as their schema
type SchemaRenderer struct {
	rootCommand *cobra.Command
}

func (r *SchemaRenderer) SetRootCommand(cmd *cobra.Command) {
}

func (r *SchemaRenderer) RenderSubsystem(w io.Writer, s *subsystem.Subsystem) error {
	return nil
}

func (r *SchemaRenderer) RenderType(w io.Writer, s *subsystem.Subsystem, typ runtime.Type, schema io.Reader) error {
	_, err := io.Copy(w, schema)
	return err
}

var _ DocRenderer = (*SchemaRenderer)(nil)
