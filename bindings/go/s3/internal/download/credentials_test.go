package download

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

func TestDownload_Credentials(t *testing.T) {
	anonymous := &credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, Anonymous: true}
	explicit := &credv1.S3Credentials{
		Type:        credv1.S3CredentialsVersionedType,
		AccessKeyID: "explicit-key", SecretAccessKey: "explicit-secret", SessionToken: "explicit-token",
	}
	environment := map[string]string{
		"AWS_ACCESS_KEY_ID": "env-key", "AWS_SECRET_ACCESS_KEY": "env-secret", "AWS_SESSION_TOKEN": "env-token",
	}
	const sharedCredentials = "[default]\naws_access_key_id=file-key\naws_secret_access_key=file-secret\naws_session_token=file-token\n"

	sources := []struct {
		name              string
		credentials       *credv1.S3Credentials
		environment       map[string]string
		sharedCredentials string
		wantAccessKey     string
		wantToken         string
	}{
		{name: "anonymous", credentials: anonymous},
		{name: "anonymous with environment", credentials: anonymous, environment: environment},
		{name: "anonymous with shared file", credentials: anonymous, sharedCredentials: sharedCredentials},
		{name: "environment", environment: environment, wantAccessKey: "env-key", wantToken: "env-token"},
		{name: "shared file", sharedCredentials: sharedCredentials, wantAccessKey: "file-key", wantToken: "file-token"},
		{name: "explicit overrides environment", credentials: explicit, environment: environment, wantAccessKey: "explicit-key", wantToken: "explicit-token"},
	}
	responses := []struct {
		name   string
		status int
		code   string
	}{
		{name: "success"},
		{name: "access denied", status: http.StatusForbidden, code: "AccessDenied"},
		{name: "invalid access key", status: http.StatusForbidden, code: "InvalidAccessKeyId"},
		{name: "invalid signature", status: http.StatusForbidden, code: "SignatureDoesNotMatch"},
		{name: "expired token", status: http.StatusForbidden, code: "ExpiredToken"},
	}

	for _, source := range sources {
		t.Run(source.name, func(t *testing.T) {
			for _, response := range responses {
				t.Run(response.name, func(t *testing.T) {
					r := require.New(t)
					withoutAWSEnvironment(t)
					for key, value := range source.environment {
						t.Setenv(key, value)
					}
					if source.sharedCredentials != "" {
						path := filepath.Join(t.TempDir(), "credentials")
						r.NoError(os.WriteFile(path, []byte(source.sharedCredentials), 0o600))
						t.Setenv("AWS_SHARED_CREDENTIALS_FILE", path)
					}
					object := s3Object{body: []byte("public object"), errStatus: response.status, errCode: response.code}
					srv := newFakeS3(t, object)
					opts := []Option{WithTempDir(t.TempDir())}
					if source.credentials != nil {
						opts = append(opts, WithCredentials(source.credentials))
					}
					out, err := Download(t.Context(), Request{
						BucketName: "bucket", ObjectKey: "object", Endpoint: srv.URL, UsePathStyle: true,
					}, opts...)
					if response.code == "" {
						r.NoError(err)
						r.Equal(object.body, readBlob(t, out.Blob))
					} else {
						r.ErrorContains(err, response.code)
						r.Nil(out)
					}
					requests := srv.recorded()
					r.Len(requests, 1, "S3 errors must not cause anonymous retries")
					header := requests[0].header
					if source.wantAccessKey == "" {
						r.Empty(header.Get("Authorization"))
						r.Empty(header.Get("X-Amz-Date"))
					} else {
						r.Contains(header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential="+source.wantAccessKey+"/")
						r.NotEmpty(header.Get("X-Amz-Date"))
					}
					r.Equal(source.wantToken, header.Get("X-Amz-Security-Token"))
				})
			}
		})
	}
}

func TestDownload_CredentialErrorsDoNotBecomeAnonymous(t *testing.T) {
	tests := []struct {
		name          string
		credentials   *credv1.S3Credentials
		providerError bool
		wantErr       string
	}{
		{name: "access key only", credentials: &credv1.S3Credentials{AccessKeyID: "key"}},
		{name: "secret key only", credentials: &credv1.S3Credentials{SecretAccessKey: "secret"}},
		{name: "session token only", credentials: &credv1.S3Credentials{SessionToken: "token"}},
		{name: "absent", wantErr: "failed to refresh cached credentials"},
		{name: "anonymous false without keys", credentials: &credv1.S3Credentials{}, wantErr: "failed to refresh cached credentials"},
		{name: "configured provider error", providerError: true, wantErr: "missing-token"},
		{name: "anonymous with keys", credentials: &credv1.S3Credentials{Anonymous: true, AccessKeyID: "key", SecretAccessKey: "secret", SessionToken: "token"}, wantErr: "anonymous S3 credentials cannot be combined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			withoutAWSEnvironment(t)
			if tt.providerError {
				t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", filepath.Join(t.TempDir(), "missing-token"))
				t.Setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/test")
			}
			var creds *credv1.S3Credentials
			if tt.credentials != nil {
				creds = tt.credentials.DeepCopy()
				creds.Type = credv1.S3CredentialsVersionedType
			}
			srv := newFakeS3(t, s3Object{body: []byte("public")})
			opts := []Option{WithCredentials(nil)}
			if creds != nil {
				opts = append(opts, WithCredentials(creds))
			}
			out, err := downloadFrom(t, srv, Request{BucketName: "bucket", ObjectKey: "key"}, opts...)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
			} else {
				var empty *credentials.StaticCredentialsEmptyError
				r.ErrorAs(err, &empty)
			}
			r.Nil(out)
			r.Empty(srv.recorded())
		})
	}
}
