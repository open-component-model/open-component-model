package credentials

import (
	signeridentityv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/identity/signer/v1"
	verifieridentityv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/identity/verifier/v1"
)

// Deprecated: Use signeridentityv1.V1Alpha1Type instead.
var IdentityTypeSigstoreSigner = signeridentityv1.V1Alpha1Type

// Deprecated: Use verifieridentityv1.V1Alpha1Type instead.
var IdentityTypeSigstoreVerifier = verifieridentityv1.V1Alpha1Type
