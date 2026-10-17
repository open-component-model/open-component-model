package layout

import "fmt"

// ComponentVersionLayout selects how a component version is stored in an OCI repository.
// The empty value means "auto": keep the layout of the version already stored under the
// same tag, and use ComponentVersionLayoutV2 for new versions.
type ComponentVersionLayout string

const (
	// ComponentVersionLayoutV2 stores the access-bearing component descriptor manifest as the tag target.
	ComponentVersionLayoutV2 ComponentVersionLayout = "v2"
	// ComponentVersionLayoutNormalized stores a stable, access-free normalized descriptor manifest as
	// the tag target (cosign-signable); the access-bearing descriptor is stored as its referrer.
	ComponentVersionLayoutNormalized ComponentVersionLayout = "normalized"
)

// Validate returns an error for any value other than "", "v2" and "normalized".
func (l ComponentVersionLayout) Validate() error {
	switch l {
	case "", ComponentVersionLayoutV2, ComponentVersionLayoutNormalized:
		return nil
	default:
		return fmt.Errorf("unknown component version layout %q (supported: %q, %q)", string(l), ComponentVersionLayoutV2, ComponentVersionLayoutNormalized)
	}
}
