package component_version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	streamConfig = `---
type: generic.config.ocm.software/v1
configurations: []
`
	streamTransferSpec = `---
environment: {}
transformations:
- id: upload
  spec:
    repository:
      type: OCIRepository/v1
  type: OCIAddComponentVersion/v1alpha1
`
)

func TestLoadTransferSpecFromStdinStream(t *testing.T) {
	tests := []struct {
		name    string
		stdin   string
		wantErr string
	}{
		{name: "transfer spec only", stdin: streamTransferSpec},
		{name: "configuration then transfer spec", stdin: streamConfig + streamTransferSpec, wantErr: "configuration is not allowed"},
		{name: "transfer spec then configuration", stdin: streamTransferSpec + streamConfig, wantErr: "configuration is not allowed"},
		{name: "empty stdin", stdin: "", wantErr: "no transfer spec document found"},
		{name: "two transfer specs", stdin: streamTransferSpec + streamTransferSpec, wantErr: "exactly one transfer spec"},
		{name: "malformed document", stdin: "this is: [not valid\n", wantErr: "parsing transfer spec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			tgd, err := loadTransferSpec("-", strings.NewReader(tt.stdin))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Len(tgd.Transformations, 1)
			r.Equal("upload", tgd.Transformations[0].ID)
			r.Equal("OCIAddComponentVersion/v1alpha1", tgd.Transformations[0].Type.String())
		})
	}
}

func TestLoadTransferSpecFileRejectsConfig(t *testing.T) {
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "spec.yaml")
	r.NoError(os.WriteFile(path, []byte(streamConfig+streamTransferSpec), 0o600))

	_, err := loadTransferSpec(path, strings.NewReader(""))
	r.ErrorContains(err, "configuration is not allowed in a transfer spec file")
}
