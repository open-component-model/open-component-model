package v1

import "testing"

func TestRelativeOCIReference_Validate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reference string
		wantErr   bool
	}{
		{name: "tag only", reference: "ocm/value:v2.0"},
		{name: "single segment", reference: "value:v2.0"},
		{name: "multi-segment", reference: "ocm-prefix/images/app:v1"},
		{name: "dotted first segment is a path", reference: "acme.org/value:v2.0"},
		{name: "digest only", reference: "ocm/value@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
		{name: "tag and digest", reference: "ocm/value:v2.0@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},

		{name: "empty", reference: "", wantErr: true},
		{name: "invalid syntax", reference: "ocm/value:bad:tag:form", wantErr: true},
		{name: "invalid digest", reference: "ocm/value@sha256:nothex", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (&RelativeOCIReference{Reference: tc.reference}).Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.reference)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.reference, err)
			}
		})
	}
}
