package v1

import (
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// ValidateDigests applies the OCM signing digest rules to cd. A signature over the normalized
// manifest covers resource content and referenced component versions only through their digests,
// so every component reference and every resource with a usable access must carry a complete
// SHA-256/SHA-512 digest, and a resource without access must not claim one. The hash algorithm
// check is enforced regardless of FIPS mode, because there is no unsigned fallback in this layout.
func ValidateDigests(cd *descruntime.Descriptor) error {
	if err := signing.IsSafelyDigestible(&cd.Component); err != nil {
		return fmt.Errorf("normalized layout requires a safely digestible component version: %w", err)
	}
	if err := signing.ValidateDigestHashAlgorithms(&cd.Component); err != nil {
		return fmt.Errorf("normalized layout requires approved digest hash algorithms: %w", err)
	}
	return nil
}
