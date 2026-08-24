package v1

import "ocm.software/open-component-model/bindings/go/runtime"

const S3CredentialsType = "S3Credentials"

var S3CredentialsVersionedType = runtime.NewVersionedType(S3CredentialsType, Version)

// MustRegisterCredentialType registers S3Credentials/v1 (and its unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&S3Credentials{},
		S3CredentialsVersionedType,
		runtime.NewUnversionedType(S3CredentialsType),
	)
}

// S3Credentials represents typed credentials for S3 authentication.
//
// When no credentials are supplied, the AWS default credential chain is used
// (environment variables, shared config, and IAM instance/task roles), so static
// keys are optional for in-cluster or environment-based setups.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type S3Credentials struct {
	// +ocm:jsonschema-gen:enum=S3Credentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3Credentials
	Type runtime.Type `json:"type"`
	// AccessKeyID is the access key ID.
	AccessKeyID string `json:"accessKeyId,omitempty"`
	// SecretAccessKey is the secret access key paired with AccessKeyID.
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	// SessionToken is an optional session token for temporary credentials.
	SessionToken string `json:"sessionToken,omitempty"`
}
