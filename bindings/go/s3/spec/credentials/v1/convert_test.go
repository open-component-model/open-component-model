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
	for _, tt := range []struct {
		name      string
		data      string
		anonymous bool
	}{
		{"omitted", `{"type":"S3Credentials/v1"}`, false},
		{"false", `{"type":"S3Credentials/v1","anonymous":false}`, false},
		{"true", `{"type":"S3Credentials/v1","anonymous":true}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			raw := &runtime.Raw{}
			r.NoError(json.Unmarshal([]byte(tt.data), raw))
			out, err := ConvertToS3Credentials(raw)
			r.NoError(err)
			r.Equal(tt.anonymous, out.Anonymous)
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
	tests := []struct {
		name      string
		value     string
		anonymous bool
	}{
		{name: "true", value: "true", anonymous: true},
		{name: "false", value: "false"},
		{name: "invalid defaults to false", value: "invalid"},
		{name: "empty defaults to false", value: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			props := map[string]string{"anonymous": tt.value}
			if !tt.anonymous {
				props["accessKeyId"] = "key"
			}
			out, err := ConvertToS3Credentials(directCredentials(props))
			r.NoError(err)
			r.Equal(tt.anonymous, out.Anonymous)
			r.Equal(props["accessKeyId"], out.AccessKeyID)
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
