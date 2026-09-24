package transformer

import (
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
)

// missingSubjectsConfigurable is implemented by resource repositories that can
// derive a copy which skips missing subjects and referrers.
type missingSubjectsConfigurable interface {
	WithAllowMissingSubjects(bool) ocistream.ResourceRepository
}
