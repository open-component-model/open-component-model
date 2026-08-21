package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func directCredentials(props map[string]string) *directcredsv1.DirectCredentials {
	return &directcredsv1.DirectCredentials{
		Type:       runtime.NewVersionedType(directcredsv1.CredentialsType, directcredsv1.Version),
		Properties: props,
	}
}

func Test_ConvertToS3Credentials_Typed(t *testing.T) {
	in := &S3Credentials{
		Type:            S3CredentialsVersionedType,
		AccessKeyID:     "AKIA",
		SecretAccessKey: "secret",
		SessionToken:    "session",
	}
	out, err := ConvertToS3Credentials(in)
	require.NoError(t, err)
	require.Equal(t, "AKIA", out.AccessKeyID)
	require.Equal(t, "secret", out.SecretAccessKey)
	require.Equal(t, "session", out.SessionToken)
}

func Test_ConvertToS3Credentials_DirectCurrentProperties(t *testing.T) {
	out, err := ConvertToS3Credentials(directCredentials(map[string]string{
		"accessKeyId":     "AKIA",
		"secretAccessKey": "secret",
		"sessionToken":    "session",
	}))
	require.NoError(t, err)
	require.Equal(t, "AKIA", out.AccessKeyID)
	require.Equal(t, "secret", out.SecretAccessKey)
	require.Equal(t, "session", out.SessionToken)
}

func Test_ConvertToS3Credentials_LegacyOCMv1Properties(t *testing.T) {
	// ocmv1 property names: awsAccessKeyID / awsSecretAccessKey, and "token" as an
	// alternative that maps to the AWS session token.
	out, err := ConvertToS3Credentials(directCredentials(map[string]string{
		"awsAccessKeyID":     "AKIA",
		"awsSecretAccessKey": "secret",
		"token":              "legacy-token",
	}))
	require.NoError(t, err)
	require.Equal(t, "AKIA", out.AccessKeyID)
	require.Equal(t, "secret", out.SecretAccessKey)
	require.Equal(t, "legacy-token", out.SessionToken)
}

func Test_ConvertToS3Credentials_CurrentPropertiesWinOverLegacy(t *testing.T) {
	out, err := ConvertToS3Credentials(directCredentials(map[string]string{
		"accessKeyId":    "new",
		"awsAccessKeyID": "old",
		"sessionToken":   "new-session",
		"token":          "old-token",
	}))
	require.NoError(t, err)
	require.Equal(t, "new", out.AccessKeyID)
	require.Equal(t, "new-session", out.SessionToken)
}
