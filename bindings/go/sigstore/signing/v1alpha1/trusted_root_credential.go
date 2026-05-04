package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

// CredentialTypeTrustedRoot is the credential type for Sigstore trusted-root material
// (Fulcio CA chain, Rekor log keys, timestamp authority certificates).
var CredentialTypeTrustedRoot = runtime.NewVersionedType("TrustedRoot", Version)
