package spec

// Recursive controls whether component references are transferred along with
// their parent component, and how deep that recursion goes: -1 means infinite
// recursion, 0 means no recursion. Values above 0 are reserved for
// depth-limited recursion, which is not implemented yet; [Config.Validate]
// rejects them.
//
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/Recursive.schema.json
type Recursive int

const (
	// RecursiveNone disables recursion; only the parent component is transferred.
	RecursiveNone Recursive = 0

	// RecursiveInfinite recurses through all component references without limit.
	RecursiveInfinite Recursive = -1
)
