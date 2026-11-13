package hooks

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
)

func TestNonTextLogRenderer(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "no flag renders text",
			args: nil,
			want: false,
		},
		{
			// text is the default format, so requesting it explicitly must
			// behave exactly like omitting the flag.
			name: "explicit text renders text",
			args: []string{"--" + log.FormatFlagName, log.FormatText},
			want: false,
		},
		{
			name: "json is another renderer",
			args: []string{"--" + log.FormatFlagName, log.FormatJSON},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			cmd := &cobra.Command{Use: "ocm", RunE: func(*cobra.Command, []string) error { return nil }}
			log.RegisterLoggingFlags(cmd.PersistentFlags())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			r.NoError(cmd.Execute())

			nonText, err := nonTextLogRenderer(cmd)
			r.NoError(err)
			r.Equal(tt.want, nonText)
		})
	}
}

func TestNonTextLogRenderer_UnregisteredFlag(t *testing.T) {
	r := require.New(t)

	nonText, err := nonTextLogRenderer(&cobra.Command{Use: "ocm"})
	r.Error(err)
	r.False(nonText)
}
