package hooks

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
)

func TestLogFormatRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "no flag keeps the progress display",
			args: nil,
			want: false,
		},
		{
			// text is the default format, but passing it still asks for log
			// records instead of the progress display.
			name: "explicit text requests log records",
			args: []string{"--" + log.FormatFlagName, log.FormatText},
			want: true,
		},
		{
			name: "explicit json requests log records",
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

			r.Equal(tt.want, logFormatRequested(cmd))
		})
	}
}
