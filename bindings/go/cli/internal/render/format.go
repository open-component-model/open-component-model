package render

import "fmt"

type OutputFormat int

const (
	OutputFormatJSON OutputFormat = iota
	OutputFormatYAML
	OutputFormatNDJSON
	OutputFormatTree
	OutputFormatTable
)

func (o OutputFormat) String() string {
	switch o {
	case OutputFormatJSON:
		return "json"
	case OutputFormatYAML:
		return "yaml"
	case OutputFormatNDJSON:
		return "ndjson"
	case OutputFormatTree:
		return "tree"
	case OutputFormatTable:
		return "table"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}
