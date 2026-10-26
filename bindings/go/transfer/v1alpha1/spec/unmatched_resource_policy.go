package spec

// UnmatchedResourcePolicy decides what happens to a resource that no uploader
// configuration selects. Such a resource follows the baseline: a local blob is
// copied as a local blob, anything else stays by reference.
//
// The zero value means [UnmatchedResourcePolicyWarn].
// +ocm:jsonschema-gen:enum=Ignore,Warn,Fail
type UnmatchedResourcePolicy string

const (
	// UnmatchedResourcePolicyIgnore applies the baseline without reporting it beyond debug logs.
	UnmatchedResourcePolicyIgnore UnmatchedResourcePolicy = "Ignore"
	// UnmatchedResourcePolicyWarn applies the baseline and logs a warning per resource
	// when at least one uploader is configured. This is the default.
	UnmatchedResourcePolicyWarn UnmatchedResourcePolicy = "Warn"
	// UnmatchedResourcePolicyFail fails the transfer, naming every resource of a component
	// version no uploader selects, also when no uploader is configured at all.
	UnmatchedResourcePolicyFail UnmatchedResourcePolicy = "Fail"
)

// Normalize resolves the zero value to the default [UnmatchedResourcePolicyWarn].
func (p UnmatchedResourcePolicy) Normalize() UnmatchedResourcePolicy {
	if p == "" {
		return UnmatchedResourcePolicyWarn
	}
	return p
}

// Valid reports whether p is empty (default) or one of the known policies.
func (p UnmatchedResourcePolicy) Valid() bool {
	switch p {
	case "", UnmatchedResourcePolicyIgnore, UnmatchedResourcePolicyWarn, UnmatchedResourcePolicyFail:
		return true
	default:
		return false
	}
}
