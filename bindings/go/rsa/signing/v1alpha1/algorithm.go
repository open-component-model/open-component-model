package v1alpha1

// SignatureAlgorithm is the signature algorithm to use when creating new signatures.
// This field is optional and defaults to AlgorithmRSASSAPSS. For verification, this field is ignored
// and the signature algorithm is inferred from the signature specification.
// +ocm:jsonschema-gen:enum=RSASSA-PSS,RSASSA-PKCS1-V1_5
type SignatureAlgorithm string
