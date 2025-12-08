package fieldpath

import (
	"fmt"
	"strconv"
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
		if segment.Index != nil {
			b.WriteString(fmt.Sprintf("[%d]", *segment.Index))
			continue
		}
		if segment.Name == "" {
			continue
		}
		if i > 0 {
			b.WriteRune('.')
		}
		if strings.ContainsRune(segment.Name, '.') ||
			strings.ContainsRune(segment.Name, '[') ||
			strings.ContainsRune(segment.Name, ']') {
			b.WriteString(strconv.Quote(segment.Name))
		} else {
			b.WriteString(segment.Name)
		}
	}

	return b.String()
}
