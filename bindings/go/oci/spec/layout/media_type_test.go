package layout

import "testing"

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
			if got := IsOCILayoutMediaType(tt.mediaType); got != tt.want {
				t.Errorf("IsOCILayoutMediaType(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}
