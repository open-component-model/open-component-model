package v1

import (
	"fmt"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyAccessKeyID     = "accessKeyId"
	credentialKeySecretAccessKey = "secretAccessKey"
	credentialKeySessionToken    = "sessionToken"

	// legacy ocmv1 credential property names, accepted as aliases.
	legacyKeyAccessKeyID     = "awsAccessKeyID"
	legacyKeySecretAccessKey = "awsSecretAccessKey"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&S3Credentials{},
		S3CredentialsVersionedType,
		runtime.NewUnversionedType(S3CredentialsType),
	)
	directcredsv1.MustRegister(convertScheme)
}

// fromDirectCredentials converts a DirectCredentials properties map into typed S3Credentials.
// This supports legacy .ocmconfig files that use Credentials/v1 with S3 properties.
func fromDirectCredentials(properties map[string]string) *S3Credentials {
	accessKeyID := properties[credentialKeyAccessKeyID]
	if accessKeyID == "" {
		accessKeyID = properties[legacyKeyAccessKeyID]
	}
	secretAccessKey := properties[credentialKeySecretAccessKey]
	if secretAccessKey == "" {
		secretAccessKey = properties[legacyKeySecretAccessKey]
	}
	return &S3Credentials{
		Type:            runtime.NewVersionedType(S3CredentialsType, Version),
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    properties[credentialKeySessionToken],
	}
}

// ConvertToS3Credentials converts runtime.Typed into S3Credentials.
// Direct conversion as well as converting from DirectCredentials/v1 is supported.
func ConvertToS3Credentials(creds runtime.Typed) (*S3Credentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	switch t := typed.(type) {
	case *directcredsv1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *S3Credentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}
