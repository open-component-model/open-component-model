package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// IdentityType is the name of the generic typed consumer identity.
	// It is the typed parallel to the untyped runtime.Identity map and is used
	// when no domain-specific identity type is available.
	IdentityType = "Identity"
	// Version is the current version of the typed Identity.
	Version = "v1"
)

// Type is the unversioned consumer identity type for the generic typed Identity (backward compat).
var Type = runtime.NewUnversionedType(IdentityType)

// VersionedType is the versioned consumer identity type for the generic typed Identity.
var VersionedType = runtime.NewVersionedType(IdentityType, Version)
