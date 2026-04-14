// Package transformer provides OCI artifact transformation capabilities.
//
// This package enables extraction and transformation of OCI artifacts with
// media-type-specific handling for various content types, including Helm charts.
//
// Note: The repo-based transformers (GetComponentVersion, AddComponentVersion,
// GetLocalResource, AddLocalResource) duplicate the Scheme.NewObject +
// Scheme.Convert + type-switch sequence between GetCredentialConsumerIdentities
// and Transform. This is intentional for now — Transform needs many more fields
// from the type-switch than GetCredentialConsumerIdentities, so a shared helper
// would require an awkward intermediate struct. If the number of transformers
// grows, consider extracting the common deserialization.
package transformer

// Credential slot names used in GetCredentialConsumerIdentities and Transform.
// Defining them as constants prevents silent mismatches between the two methods.
const (
	// CredentialSlotRepository identifies credentials for a component version repository.
	CredentialSlotRepository = "repository"
	// CredentialSlotResource identifies credentials for a resource (e.g. OCI artifact).
	CredentialSlotResource = "resource"
)
