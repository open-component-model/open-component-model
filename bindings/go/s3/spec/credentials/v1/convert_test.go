package v1

import (
	"encoding/json"
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

func Test_ConvertToS3Credentials_Anonymous(t *testing.T) {
	for _, data := range []string{
		`{"type":"S3Credentials/v1"}`,
		`{"type":"S3Credentials/v1","anonymous":false}`,
		`{"type":"S3Credentials/v1","anonymous":true}`,
	} {
		t.Run(data, func(t *testing.T) {
			r := require.New(t)
			raw := &runtime.Raw{}
			r.NoError(json.Unmarshal([]byte(data), raw))
			out, err := ConvertToS3Credentials(raw)
			r.NoError(err)
			r.Equal(data == `{"type":"S3Credentials/v1","anonymous":true}`, out.Anonymous)
			encoded, err := json.Marshal(out)
			r.NoError(err)
			if out.Anonymous {
				r.Contains(string(encoded), `"anonymous":true`)
			} else {
				r.NotContains(string(encoded), "anonymous")
			}
			roundTrip, err := ConvertToS3Credentials(out)
			r.NoError(err)
			r.Equal(out, roundTrip)
		})
	}
}

func Test_ConvertToS3Credentials_AnonymousConflicts(t *testing.T) {
	for _, key := range []string{"accessKeyId", "secretAccessKey", "sessionToken", "awsAccessKeyID", "awsSecretAccessKey", "token"} {
		t.Run(key, func(t *testing.T) {
			r := require.New(t)
			out, err := ConvertToS3Credentials(directCredentials(map[string]string{"anonymous": "true", key: "value"}))
			r.ErrorContains(err, "anonymous S3 credentials cannot be combined")
			r.Nil(out)
			if key == "accessKeyId" || key == "secretAccessKey" || key == "sessionToken" {
				data, err := json.Marshal(map[string]any{"type": "S3Credentials/v1", "anonymous": true, key: "value"})
				r.NoError(err)
				creds := &S3Credentials{}
				r.NoError(json.Unmarshal(data, creds))
				out, err = ConvertToS3Credentials(creds)
				r.ErrorContains(err, "anonymous S3 credentials cannot be combined")
				r.Nil(out)
			}
			out, err = ConvertToS3Credentials(directCredentials(map[string]string{"anonymous": "false", key: "value"}))
			r.NoError(err)
			r.False(out.Anonymous)
		})
	}
}

func Test_ConvertToS3Credentials_DirectAnonymous(t *testing.T) {
	for _, value := range []string{"true", "false", "invalid", ""} {
		t.Run(value, func(t *testing.T) {
			r := require.New(t)
			out, err := ConvertToS3Credentials(directCredentials(map[string]string{"anonymous": value}))
			if value == "invalid" || value == "" {
				r.ErrorContains(err, "invalid anonymous credential property")
				r.Nil(out)
			} else {
				r.NoError(err)
				r.Equal(value == "true", out.Anonymous)
			}
		})
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
