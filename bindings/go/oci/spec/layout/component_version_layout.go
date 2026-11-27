package layout

import "fmt"

// ComponentVersionLayout selects how a component version is stored in an OCI repository.
// The empty value means ComponentVersionLayoutDefault.
type ComponentVersionLayout string

const (
	// ComponentVersionLayoutDefault stores the access-bearing component descriptor manifest (artifact
	// type component-descriptor.v2) as the tag target.
	ComponentVersionLayoutDefault ComponentVersionLayout = "default"
	// ComponentVersionLayoutNormalized (experimental) stores a stable, access-free normalized descriptor
	// manifest as the tag target (cosign-signable); the access-bearing descriptor is stored as its referrer.
	ComponentVersionLayoutNormalized ComponentVersionLayout = "normalized"
)

// Validate returns an error for any value other than "", "default" and "normalized".
func (l ComponentVersionLayout) Validate() error {
	switch l {
	case "", ComponentVersionLayoutDefault, ComponentVersionLayoutNormalized:
		return nil
	default:
		return fmt.Errorf("unknown component version layout %q (supported: %q, %q)", string(l), ComponentVersionLayoutDefault, ComponentVersionLayoutNormalized)
	}
}
