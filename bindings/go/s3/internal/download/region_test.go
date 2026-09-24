package download

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

type regionTransport func(*http.Request) (*http.Response, error)

func (f regionTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func regionResponse(req *http.Request, status int, code, region string) *http.Response {
	body := "regional object"
	if code != "" {
		body = fmt.Sprintf("<Error><Code>%s</Code><Message>test error</Message></Error>", code)
	}
	if req.Method == http.MethodHead {
		body = ""
	}
	header := http.Header{"X-Amz-Bucket-Region": {region}, "X-Amz-Version-Id": {"pinned/+version"}}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

func TestDownload_RegionCorrection(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		for _, headStatus := range []int{0, http.StatusOK, http.StatusForbidden, http.StatusMovedPermanently} {
			discovery := headStatus != 0
			for _, pathStyle := range []bool{false, true} {
				t.Run(fmt.Sprintf("anonymous=%t/headStatus=%d/pathStyle=%t", anonymous, headStatus, pathStyle), func(t *testing.T) {
					r := require.New(t)
					withoutAWSEnvironment(t)
					t.Setenv("AWS_REGION", "ap-south-1")
					creds := &credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, Anonymous: anonymous}
					if !anonymous {
						creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken = "key", "secret", "token"
					}
					var methods []string
					client := &http.Client{Transport: regionTransport(func(req *http.Request) (*http.Response, error) {
						methods = append(methods, req.Method)
						region := "us-east-1"
						if len(methods) > 1 && req.Method == http.MethodGet {
							region = "eu-west-1"
						}
						host := "s3." + region + ".amazonaws.com"
						path := "/folder/a +%/object"
						if pathStyle {
							path = "/test-bucket" + path
						} else {
							host = "test-bucket." + host
						}
						r.Equal("https", req.URL.Scheme)
						r.Equal(host, req.URL.Host)
						if anonymous {
							r.Empty(req.Header.Get("Authorization"))
							r.Empty(req.Header.Get("X-Amz-Security-Token"))
						} else {
							r.Contains(req.Header.Get("Authorization"), "/"+region+"/s3/aws4_request")
							r.Contains(req.Header.Get("Authorization"), "Credential=key/")
							r.Equal("token", req.Header.Get("X-Amz-Security-Token"))
						}
						if req.Method == http.MethodHead {
							return regionResponse(req, headStatus, "", "eu-west-1"), nil
						}
						r.Equal(path, req.URL.Path)
						r.Equal("pinned/+version", req.URL.Query().Get("versionId"))
						if len(methods) == 1 {
							hint := "eu-west-1"
							if discovery {
								hint = ""
							}
							return regionResponse(req, 301, "PermanentRedirect", hint), nil
						}
						return regionResponse(req, 200, "", ""), nil
					})}
					out, err := Download(t.Context(), Request{Region: "us-east-1", BucketName: "test-bucket", ObjectKey: "folder/a +%/object", Version: "pinned/+version", UsePathStyle: pathStyle}, WithHTTPClient(client), WithCredentials(creds), WithTempDir(t.TempDir()))
					r.NoError(err)
					r.Equal("regional object", string(readBlob(t, out.Blob)))
					r.Equal("pinned/+version", out.VersionID)
					want := []string{"GET", "GET"}
					if discovery {
						want = []string{"GET", "HEAD", "GET"}
					}
					r.Equal(want, methods)
				})
			}
		}
	}
}

func TestDownload_RegionCorrectionErrors(t *testing.T) {
	tests := []struct {
		name, endpoint, hint, code      string
		status, headStatus, retryStatus int
		headHint, wantError             string
		wantMethods                     []string
		transportError                  bool
	}{
		{name: "custom endpoint", endpoint: "https://custom.example", hint: "eu-west-1", status: 301, code: "PermanentRedirect", wantError: "PermanentRedirect", wantMethods: []string{"GET"}},
		{name: "forbidden", hint: "eu-west-1", status: 403, code: "AccessDenied", wantError: "AccessDenied", wantMethods: []string{"GET"}},
		{name: "not found", hint: "eu-west-1", status: 404, code: "NoSuchKey", wantError: "NoSuchKey", wantMethods: []string{"GET"}},
		{name: "auth error", hint: "eu-west-1", status: 400, code: "AuthorizationHeaderMalformed", wantError: "AuthorizationHeaderMalformed", wantMethods: []string{"GET"}},
		{name: "other redirect", hint: "eu-west-1", status: 301, code: "OtherError", wantError: "OtherError", wantMethods: []string{"GET"}},
		{name: "injection", hint: "eu-west-1.attacker.example/", status: 301, code: "PermanentRedirect", wantError: "invalid bucket region", wantMethods: []string{"GET"}},
		{name: "same region", hint: "us-east-1", status: 301, code: "PermanentRedirect", wantError: "already attempted", wantMethods: []string{"GET"}},
		{name: "missing hint", status: 301, code: "PermanentRedirect", headStatus: 200, wantError: "missing or invalid", wantMethods: []string{"GET", "HEAD"}},
		{name: "discovery denied", status: 301, code: "PermanentRedirect", headStatus: 403, wantError: "HeadBucket", wantMethods: []string{"GET", "HEAD"}},
		{name: "invalid discovered hint", status: 301, code: "PermanentRedirect", headStatus: 200, headHint: "https://evil.example", wantError: "invalid bucket region", wantMethods: []string{"GET", "HEAD"}},
		{name: "bounded redirect", hint: "eu-west-1", status: 301, code: "PermanentRedirect", retryStatus: 301, wantError: "retrying in bucket region", wantMethods: []string{"GET", "GET"}},
		{name: "retry denied", hint: "eu-west-1", status: 301, code: "PermanentRedirect", retryStatus: 403, wantError: "retrying in bucket region", wantMethods: []string{"GET", "GET"}},
		{name: "transport failure", transportError: true, wantError: "transport failed", wantMethods: []string{"GET"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			withoutAWSEnvironment(t)
			var methods []string
			client := &http.Client{Transport: regionTransport(func(req *http.Request) (*http.Response, error) {
				methods = append(methods, req.Method)
				if tt.transportError {
					return nil, errors.New("transport failed")
				}
				if req.Method == http.MethodHead {
					return regionResponse(req, tt.headStatus, "", tt.headHint), nil
				}
				if len(methods) > 1 {
					return regionResponse(req, tt.retryStatus, "PermanentRedirect", "ap-south-1"), nil
				}
				return regionResponse(req, tt.status, tt.code, tt.hint), nil
			})}
			out, err := Download(t.Context(), Request{BucketName: "test-bucket", ObjectKey: "object", Endpoint: tt.endpoint}, WithHTTPClient(client), WithTempDir(t.TempDir()), WithCredentials(&credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, Anonymous: true}), WithHTTPConfig(&httpv1alpha1.Config{Retry: &httpv1alpha1.RetryConfig{MaxRetries: new(-1)}}))
			r.Nil(out)
			r.ErrorContains(err, tt.wantError)
			r.Equal(tt.wantMethods, methods)
		})
	}
}
