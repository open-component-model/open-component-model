package render

import "fmt"

type OutputFormat int

const (
	OutputFormatJSON OutputFormat = iota
	OutputFormatYAML
	OutputFormatNDJSON
)

func (o OutputFormat) String() string {
	switch o {
	case OutputFormatJSON:
		return "json"
	case OutputFormatYAML:
		return "yaml"
	case OutputFormatNDJSON:
		return "ndjson"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}
