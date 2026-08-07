// Package policy defines shared policy types for the OCI library.
package policy

// GlobalAccessPolicy controls whether global access references are added to local blobs.
// The zero value is GlobalAccessPolicyNever, suppressing global access by default.
type GlobalAccessPolicy int

const (
	// GlobalAccessPolicyNever suppresses global access on all local blobs, even when the storage
	// backend is globally reachable. This is the default policy to discourage reliance on
	// global access references.
	GlobalAccessPolicyNever GlobalAccessPolicy = iota
	// GlobalAccessPolicyAuto auto-detects based on the storage backend. Global access is only
	// added when the storage backend is globally reachable (e.g. a remote OCI registry).
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicyAuto
)
