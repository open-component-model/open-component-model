package transformer

import (
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// reconcileSourceDigest keeps the transfer pipeline's resource digest authoritative over
// whatever the add/upload sink produced.
//
// The sink (pack/blob) only fills a *missing* OCM resource digest from the content and does
// not re-verify a digest the source already carries, so transfer must not let an injected or
// altered digest reach the target descriptor:
//
//   - Source carried no digest -> record none. Injecting one would change the signed,
//     normalized descriptor and break a signature that excluded the resource.
//   - Source carried a digest -> record it verbatim. If the sink nonetheless produced a
//     different digest, that is a content/integrity disagreement we surface rather than
//     silently paper over.
//
// src is the digest the pipeline handed the sink (the Get output, or a prior transformation's
// new digest); out is the resource the sink returned.
func reconcileSourceDigest(src, out *v2.Resource) error {
	if src.Digest == nil {
		out.Digest = nil
		return nil
	}
	if out.Digest != nil && *out.Digest != *src.Digest {
		return fmt.Errorf("digest mismatch for resource %q: source descriptor declares %s=%s but transfer produced %s=%s",
			src.ToIdentity(),
			src.Digest.NormalisationAlgorithm, src.Digest.Value,
			out.Digest.NormalisationAlgorithm, out.Digest.Value)
	}
	out.Digest = src.Digest.DeepCopy()
	return nil
}
