package layout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOCILayoutMediaType(t *testing.T) {
	tests := []struct {
		mediaType string
		want      bool
	}{
		{MediaTypeOCIImageLayout, true},
		{MediaTypeOCIImageLayoutV1, true},
		{MediaTypeOCIImageLayoutTarV1, true},
		{MediaTypeOCIImageLayoutTarGzipV1, true},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tt.want, IsOCILayoutMediaType(tt.mediaType))
		})
	}
}
