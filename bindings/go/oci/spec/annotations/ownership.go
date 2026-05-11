package annotations

// Ownership referrer annotation keys. See docs/adr/0016_ownership_annotations.md.
//
// An ownership referrer is a minimal OCI manifest whose subject points to a
// resource manifest and whose annotations record the owning component version
// and the artifact identity.
const (
	// OwnershipComponentName is an annotation that records the plain component
	// name on an ownership referrer manifest.
	OwnershipComponentName = "software.ocm.component.name"

	// OwnershipComponentVersion is an annotation that records the plain
	// component version on an ownership referrer manifest.
	//
	// Distinct from [OCMComponentVersion] in annotations.go
	// ("software.ocm.componentversion"), which encodes "<name>:<version>" as a
	// single string and is set on component-descriptor manifests. The
	// ownership pair is split into name + version so referrer annotations
	// stay queryable as plain key/value without parsing.
	OwnershipComponentVersion = "software.ocm.component.version"
)

// OwnershipArtifactType is the OCI artifactType set on ownership referrer
// manifests. It enables filtering via the Referrers API
// (GET /v2/<name>/referrers/<digest>?artifactType=...).
const OwnershipArtifactType = "application/vnd.ocm.software.ownership.v1+json"
