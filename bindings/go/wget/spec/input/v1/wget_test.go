package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWget_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   Wget
		wantErr string
	}{
		{
			name:  "valid",
			input: Wget{URL: "https://example.com/artifact.tar.gz"},
		},
		{
			name:    "missing url",
			input:   Wget{},
			wantErr: "url is required",
		},
		{
			name:    "unsupported scheme",
			input:   Wget{URL: "ftp://example.com/artifact.tar.gz"},
			wantErr: "url must use the http or https scheme",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
