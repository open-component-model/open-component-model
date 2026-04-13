// Package transformer provides OCI artifact transformation capabilities.
//
// This package enables extraction and transformation of OCI artifacts with
// media-type-specific handling for various content types, including Helm charts.
package transformer

// Credential slot names used in GetCredentialConsumerIdentities and Transform.
// Defining them as constants prevents silent mismatches between the two methods.
const (
	// CredentialSlotRepository identifies credentials for a component version repository.
	CredentialSlotRepository = "repository"
	// CredentialSlotResource identifies credentials for a resource (e.g. OCI artifact).
	CredentialSlotResource = "resource"
)
