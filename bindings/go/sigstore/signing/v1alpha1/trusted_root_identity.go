package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

// IdentityTypeTrustedRoot is the credential consumer identity type for handlers
// that need Sigstore trusted-root material (Fulcio CA chain, Rekor log keys,
// timestamp authority certificates).
var IdentityTypeTrustedRoot = runtime.NewVersionedType("TrustedRoot", Version)
