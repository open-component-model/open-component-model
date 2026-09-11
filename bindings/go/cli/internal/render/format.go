package render

import "fmt"

type OutputFormat int

const (
	OutputFormatJSON OutputFormat = iota
	OutputFormatYAML
	OutputFormatNDJSON
	OutputFormatTree
	OutputFormatWideTree
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
	case OutputFormatWideTree:
		return "widetree"
	case OutputFormatTable:
		return "table"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}
