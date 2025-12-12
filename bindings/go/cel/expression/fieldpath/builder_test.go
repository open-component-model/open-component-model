package fieldpath

import "testing"

func TestBuild(t *testing.T) {
	tests := []struct {
		name     string
		segments []Segment
		want     string
	}{
		{
			name: "simple field",
			segments: []Segment{
				NamedSegment("spec"),
			},
			want: "spec",
		},
		{
			name: "two simple fields",
			segments: []Segment{
				NamedSegment("spec"),
				NamedSegment("containers"),
			},
			want: "spec.containers",
		},
		{
			name: "array index",
			segments: []Segment{
				NamedSegment("containers"),
				IndexedSegment(0),
			},
			want: "containers[0]",
		},
		{
			name: "dotted field name",
			segments: []Segment{
				NamedSegment("aws.eks.cluster"),
			},
			want: `"aws.eks.cluster"`,
		},
		{
			name: "mixed names and indices",
			segments: []Segment{
				NamedSegment("spec"),
				IndexedSegment(0),
				NamedSegment("env"),
			},
			want: "spec[0].env",
		},
		{
			name: "dotted names and indices",
			segments: []Segment{
				NamedSegment("somefield"),
				NamedSegment("labels.kubernetes.io/name"),
				IndexedSegment(0),
				NamedSegment("value"),
			},
			want: `somefield."labels.kubernetes.io/name"[0].value`,
		},
		{
			name: "consecutive indices",
			segments: []Segment{
				NamedSegment("spec"),
				IndexedSegment(0),
				IndexedSegment(2),
			},
			want: "spec[0][2]",
		},
		{
			name:     "empty segments",
			segments: []Segment{},
			want:     "",
		},
		{
			name: "mix of everything",
			segments: []Segment{
				NamedSegment("field"),
				NamedSegment("subfield"),
				IndexedSegment(0),
				NamedSegment("kubernetes.io/config"),
				NamedSegment(""), // ignored as empty
				NamedSegment("field"),
				IndexedSegment(1),
			},
			want: `field.subfield[0]."kubernetes.io/config".field[1]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Build(tt.segments)
			if got != tt.want {
				t.Errorf("Build() = %v, want %v", got, tt.want)
				return
			}
		})
	}
}
