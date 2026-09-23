package download

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

func TestDownload_Credentials(t *testing.T) {
	for _, source := range []string{"anonymous", "anonymous with environment", "anonymous with shared file", "environment", "shared file", "explicit"} {
		for _, code := range []string{"", "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken"} {
			t.Run(source+"/"+code, func(t *testing.T) {
				r := require.New(t)
				withoutAWSEnvironment(t)
				var creds *credv1.S3Credentials
				var key, token string
				switch source {
				case "environment", "explicit", "anonymous with environment":
					t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
					t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
					t.Setenv("AWS_SESSION_TOKEN", "env-token")
					key, token = "env-key", "env-token"
					if source == "explicit" {
						creds = fakeCredentials()
						creds.SessionToken = "explicit-token"
						key, token = creds.AccessKeyID, creds.SessionToken
					}
				case "shared file", "anonymous with shared file":
					path := filepath.Join(t.TempDir(), "credentials")
					r.NoError(os.WriteFile(path, []byte("[default]\naws_access_key_id=file-key\naws_secret_access_key=file-secret\naws_session_token=file-token\n"), 0o600))
					t.Setenv("AWS_SHARED_CREDENTIALS_FILE", path)
					key, token = "file-key", "file-token"
				}
				anonymous := strings.HasPrefix(source, "anonymous")
				if anonymous {
					creds = &credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, Anonymous: true}
					token = ""
				}
				object := s3Object{body: []byte("public object")}
				if code != "" {
					object.errStatus, object.errCode = http.StatusForbidden, code
				}
				srv := newFakeS3(t, object)
				opts := []Option{WithTempDir(t.TempDir())}
				if creds != nil {
					opts = append(opts, WithCredentials(creds))
				}
				out, err := Download(t.Context(), Request{
					BucketName: "bucket", ObjectKey: "object", Endpoint: srv.URL, UsePathStyle: true,
				}, opts...)
				if code == "" {
					r.NoError(err)
					r.Equal(object.body, readBlob(t, out.Blob))
				} else {
					r.ErrorContains(err, code)
					r.Nil(out)
				}
				requests := srv.recorded()
				r.Len(requests, 1, "S3 errors must not cause anonymous retries")
				header := requests[0].header
				if anonymous {
					r.Empty(header.Get("Authorization"))
					r.Empty(header.Get("X-Amz-Date"))
				} else {
					r.Contains(header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential="+key+"/")
					r.NotEmpty(header.Get("X-Amz-Date"))
				}
				r.Equal(token, header.Get("X-Amz-Security-Token"))
			})
		}
	}
}

func TestDownload_InvalidCredentialsDoNotBecomeAnonymous(t *testing.T) {
	for _, creds := range []*credv1.S3Credentials{
		{AccessKeyID: "key"}, {SecretAccessKey: "secret"}, {SessionToken: "token"},
	} {
		t.Run(creds.AccessKeyID+creds.SecretAccessKey+creds.SessionToken, func(t *testing.T) {
			r := require.New(t)
			withoutAWSEnvironment(t)
			creds.Type = credv1.S3CredentialsVersionedType
			srv := newFakeS3(t, s3Object{body: []byte("public")})
			_, err := downloadFrom(t, srv, Request{BucketName: "bucket", ObjectKey: "key"}, WithCredentials(creds))
			var empty *credentials.StaticCredentialsEmptyError
			r.ErrorAs(err, &empty)
			r.Empty(srv.recorded())
		})
	}
}

func TestDownload_ConfiguredProviderErrorDoesNotBecomeAnonymous(t *testing.T) {
	r := require.New(t)
	withoutAWSEnvironment(t)
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", filepath.Join(t.TempDir(), "missing-token"))
	t.Setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/test")
	srv := newFakeS3(t, s3Object{body: []byte("public")})
	_, err := downloadFrom(t, srv, Request{BucketName: "bucket", ObjectKey: "key"}, WithCredentials(nil))
	r.ErrorContains(err, "missing-token")
	r.Empty(srv.recorded())
}

func TestDownload_MissingCredentials(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "absent"
		if empty {
			name = "anonymous false without keys"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			withoutAWSEnvironment(t)
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			opts := []Option{WithCredentials(nil)}
			if empty {
				opts = []Option{WithCredentials(&credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType})}
			}
			srv := newFakeS3(t, s3Object{body: []byte("public")})
			out, err := downloadFrom(t, srv, Request{BucketName: "bucket", ObjectKey: "key"}, opts...)
			r.ErrorContains(err, "failed to refresh cached credentials")
			r.Nil(out)
			r.Empty(srv.recorded())
		})
	}
}

func TestDownload_AnonymousRejectsKeys(t *testing.T) {
	r := require.New(t)
	withoutAWSEnvironment(t)
	creds := fakeCredentials()
	creds.Anonymous = true
	srv := newFakeS3(t, s3Object{body: []byte("public")})
	out, err := downloadFrom(t, srv, Request{BucketName: "bucket", ObjectKey: "key"}, WithCredentials(creds))
	r.ErrorContains(err, "anonymous S3 credentials cannot be combined")
	r.Nil(out)
	r.Empty(srv.recorded())
}
