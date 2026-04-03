package v1alpha1

// SignatureAlgorithm is the signature algorithm to use when creating new signatures.
// This field is optional and defaults to AlgorithmECDSAP256. For verification, this field is ignored
// and the signature algorithm is inferred from the signature specification.
// +ocm:jsonschema-gen:enum=ECDSA-P256,ECDSA-P384,ECDSA-P521
type SignatureAlgorithm string