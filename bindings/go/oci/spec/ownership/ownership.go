// Package ownership decodes ownership referrer payloads (ADR 0016).
//
// An ownership referrer is a minimal OCI manifest with
// [annotations.OwnershipArtifactType] whose subject points to a resource
// manifest. Its annotations record the owning component version and the
// artifact identity. This package turns those annotations into a typed
// [Ownership] value. The annotation keys themselves live in
// [ocm.software/open-component-model/bindings/go/oci/spec/annotations] since
// they are part of the on-the-wire format.
package ownership

import (
	"encoding/json"
	"errors"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
)

// Ownership is the parsed payload of an ownership referrer (ADR 0016): the
// component version that owns an OCI subject artifact together with the
// artifact's identity in that component version.
type Ownership struct {
	// ComponentName is the plain component name (e.g. "ocm.software/ocmcli").
	ComponentName string
	// ComponentVersion is the plain component version (e.g. "v1.0.0").
	ComponentVersion string
	// Artifact records the owning component descriptor entry: its identity
	// and whether it is a resource or a source.
	//
	// On an ownership referrer the [annotations.ArtifactAnnotationKey] payload
	// is a single object, not the JSON array used by
	// [annotations.GetArtifactOCILayerAnnotations] on OCI layer/manifest
	// annotations.
	Artifact annotations.ArtifactOCIAnnotation
}

// ErrNotAnOwnershipReferrer signals that a descriptor does not carry the
// ownership annotation set. Callers walking the Referrers API can use it to
// skip descriptors that share [annotations.OwnershipArtifactType] but have
// no payload yet (or were written by a future schema).
var ErrNotAnOwnershipReferrer = errors.New("descriptor is not an ownership referrer")

// Parse decodes the ownership annotations on a referrer descriptor returned
// by the Referrers API. It returns [ErrNotAnOwnershipReferrer] when any of
// [annotations.OwnershipComponentName], [annotations.OwnershipComponentVersion],
// or [annotations.ArtifactAnnotationKey] is absent, and a wrapping error when
// [annotations.ArtifactAnnotationKey] is present but malformed.
func Parse(desc ociImageSpecV1.Descriptor) (Ownership, error) {
	name, hasName := desc.Annotations[annotations.OwnershipComponentName]
	version, hasVersion := desc.Annotations[annotations.OwnershipComponentVersion]
	rawArtifact, hasArtifact := desc.Annotations[annotations.ArtifactAnnotationKey]
	if !hasName || !hasVersion || !hasArtifact {
		return Ownership{}, ErrNotAnOwnershipReferrer
	}

	var artifact annotations.ArtifactOCIAnnotation
	if err := json.Unmarshal([]byte(rawArtifact), &artifact); err != nil {
		return Ownership{}, fmt.Errorf("ownership referrer %s annotation is malformed: %w", annotations.ArtifactAnnotationKey, err)
	}

	return Ownership{
		ComponentName:    name,
		ComponentVersion: version,
		Artifact:         artifact,
	}, nil
}
