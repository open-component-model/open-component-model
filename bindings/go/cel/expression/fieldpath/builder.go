package fieldpath

import (
	"fmt"
	"strings"
)

// Build constructs a field path string from a slice of segments.
//
// Examples:
//   - [{Name: "spec"}, {Name: "containers", ArrayIdx: 0}] -> spec.containers[0]
//   - [{Name: "spec"}, {Name: "my.field"}] -> spec["my.field"]
func Build(segments []Segment) string {
	var b strings.Builder

	for i, segment := range segments {
		if i > 0 && !strings.HasSuffix(b.String(), "]") {
			b.WriteByte('.')
		}

		if segment.Index != nil {
			b.WriteString(fmt.Sprintf("[%d]", segment.Index))
			continue
		}

		if strings.Contains(segment.Name, ".") {
			b.WriteString(fmt.Sprintf(`[%q]`, segment.Name))
		} else {
			b.WriteString(segment.Name)
		}
	}

	return b.String()
}
