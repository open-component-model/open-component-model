package v1

import (
	"fmt"
	"strings"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// acceptedHashAlgorithms are the digest hash algorithms strong enough for the normalized layout.
var acceptedHashAlgorithms = map[string]bool{
	"SHA-256": true,
	"SHA-384": true,
	"SHA-512": true,
}

// RequireAllResourcesDigested returns an error unless every resource carries a digest with an
// accepted (sha256+) hash algorithm. Resources explicitly excluded from the signature via the OCM
// sentinel (HashAlgorithm == NoDigest) are allowed, matching jsonNormalisation/v4alpha1 behavior
// for none-access resources. Sources are intentionally not enforced. This guarantees no unbound
// resource content in the normalized (cosign-signable) layout.
func RequireAllResourcesDigested(cd *descruntime.Descriptor) error {
	var problems []string
	for _, r := range cd.Component.Resources {
		if err := checkResourceDigest(r.Digest); err != nil {
			problems = append(problems, fmt.Sprintf("resource %q: %v", r.Name, err))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("normalized layout requires every resource to be digested: %s",
			strings.Join(problems, "; "))
	}
	return nil
}

func checkResourceDigest(d *descruntime.Digest) error {
	if d == nil {
		return fmt.Errorf("missing digest")
	}
	// Explicitly excluded-from-signature resources (e.g. none-access) are allowed.
	if d.HashAlgorithm == descruntime.NoDigest {
		return nil
	}
	if d.Value == "" {
		return fmt.Errorf("missing digest value")
	}
	if !acceptedHashAlgorithms[d.HashAlgorithm] {
		return fmt.Errorf("hash algorithm %q is not accepted (need SHA-256 or stronger)", d.HashAlgorithm)
	}
	return nil
}
