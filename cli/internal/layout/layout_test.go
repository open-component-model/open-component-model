package layout

import (
	"testing"

	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestApply(t *testing.T) {
	tests := []struct {
		name    string
		layout  string
		want    string
		wantErr bool
	}{
		{name: "normalized", layout: "normalized", want: "normalized"},
		{name: "v2 leaves empty", layout: "v2", want: ""},
		{name: "empty leaves empty", layout: "", want: ""},
		{name: "bogus errors", layout: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oci := &ocirepospec.Repository{BaseUrl: "ghcr.io"}
			ctf := &ctfrepospec.Repository{FilePath: "./archive"}

			ociErr := Apply(oci, tt.layout)
			ctfErr := Apply(ctf, tt.layout)

			if tt.wantErr {
				if ociErr == nil || ctfErr == nil {
					t.Fatalf("expected error for layout %q, got oci=%v ctf=%v", tt.layout, ociErr, ctfErr)
				}
				return
			}

			if ociErr != nil {
				t.Fatalf("unexpected error on oci spec: %v", ociErr)
			}
			if ctfErr != nil {
				t.Fatalf("unexpected error on ctf spec: %v", ctfErr)
			}
			if oci.ComponentVersionLayout != tt.want {
				t.Errorf("oci ComponentVersionLayout = %q, want %q", oci.ComponentVersionLayout, tt.want)
			}
			if ctf.ComponentVersionLayout != tt.want {
				t.Errorf("ctf ComponentVersionLayout = %q, want %q", ctf.ComponentVersionLayout, tt.want)
			}
		})
	}
}
