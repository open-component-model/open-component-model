package integration_test

import (
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

func Test_Integration_S3RegionRedirect(t *testing.T) {
	// Keep developer endpoint overrides and shared profiles out of SDK resolution.
	for key, value := range map[string]string{
		"AWS_CONFIG_FILE":                     os.DevNull,
		"AWS_SHARED_CREDENTIALS_FILE":         os.DevNull,
		"AWS_PROFILE":                         "",
		"AWS_DEFAULT_PROFILE":                 "",
		"AWS_ENDPOINT_URL":                    "",
		"AWS_ENDPOINT_URL_S3":                 "",
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "true",
		"AWS_USE_FIPS_ENDPOINT":               "false",
		"AWS_USE_DUALSTACK_ENDPOINT":          "false",
		"AWS_MAX_ATTEMPTS":                    "1",
	} {
		t.Setenv(key, value)
	}

	const (
		bucket      = "ocm-region-test"
		key         = "objects/resource.txt"
		wrongRegion = "us-west-2"
		region      = "eu-central-1"
		wrongHost   = bucket + ".s3." + wrongRegion + ".amazonaws.com"
		correctHost = bucket + ".s3." + region + ".amazonaws.com"
		wantContent = "hermetic S3 region redirect test\n"
	)

	for _, tt := range []struct {
		name   string
		access runtime.Typed
	}{
		{
			name:   "v1",
			access: &accessv1.S3{Type: accessspec.V1VersionedType, Bucket: bucket, Key: key, Region: wrongRegion},
		},
		{
			name:   "v2",
			access: &accessv2.S3{Type: accessspec.V2VersionedType, BucketName: bucket, ObjectKey: key, Region: wrongRegion},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			type exchange struct {
				request *http.Request
				status  int
				region  string
			}
			var mu sync.Mutex
			var exchanges []exchange
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				status, body := http.StatusBadRequest, "unexpected request"
				if req.Method == http.MethodGet && req.URL.Path == "/"+key {
					switch {
					case len(exchanges) == 0 && req.Host == wrongHost:
						status = http.StatusMovedPermanently
						body = `<Error><Code>PermanentRedirect</Code><Message>Use the bucket region</Message></Error>`
						w.Header().Set("Content-Type", "application/xml")
						w.Header().Set("x-amz-bucket-region", region)
					case len(exchanges) == 1 && req.Host == correctHost:
						status, body = http.StatusOK, wantContent
					}
				}
				exchanges = append(exchanges, exchange{req.Clone(req.Context()), status, w.Header().Get("x-amz-bucket-region")})
				mu.Unlock()
				w.WriteHeader(status)
				if _, err := io.WriteString(w, body); err != nil {
					t.Errorf("writing S3 response: %v", err)
				}
			}))
			t.Cleanup(srv.Close)
			client := srv.Client()
			transport := client.Transport.(*http.Transport).Clone()
			transport.Proxy = nil
			// Trust the test certificate without changing the SDK's AWS request host.
			transport.TLSClientConfig.ServerName = "example.com"
			r.False(transport.TLSClientConfig.InsecureSkipVerify)
			r.NoError(srv.Certificate().VerifyHostname("example.com"))
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
			}
			client.Transport = transport
			t.Cleanup(transport.CloseIdleConnections)

			// The repository's file-backed blob is removed by TempDir cleanup.
			tempDir := t.TempDir()
			repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir},
				repository.WithHTTPClient(client), repository.WithMaxDownloadSize(64*1024))
			resource := &descriptor.Resource{}
			resource.Access = tt.access
			before := resource.DeepCopy()
			b, err := repo.DownloadResource(ctx, resource, &credv1.S3Credentials{
				Type: credv1.S3CredentialsVersionedType, Anonymous: true,
			})
			r.NoError(err)
			r.Equal(before, resource, "region correction must not mutate the descriptor")
			reader, err := b.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { r.NoError(reader.Close()) })
			content, err := io.ReadAll(reader)
			r.NoError(err)
			r.Equal(wantContent, string(content))
			r.Equal(sha256.Sum256([]byte(wantContent)), sha256.Sum256(content))

			mu.Lock()
			defer mu.Unlock()
			r.Len(exchanges, 2, "expected only wrong-region and corrected GetObject requests")
			r.Equal(http.StatusMovedPermanently, exchanges[0].status)
			r.Equal(region, exchanges[0].region)
			r.Equal(wrongHost, exchanges[0].request.Host)
			r.Equal(http.StatusOK, exchanges[1].status)
			r.Equal(correctHost, exchanges[1].request.Host)
			for _, e := range exchanges {
				r.Equal(http.MethodGet, e.request.Method)
				r.NotNil(e.request.TLS)
				r.Equal("/"+key, e.request.URL.Path)
				r.Empty(e.request.Header.Get("Authorization"))
				r.Empty(e.request.Header.Get("X-Amz-Security-Token"))
				r.Empty(e.request.URL.Query().Get("X-Amz-Credential"))
				r.Empty(e.request.URL.Query().Get("X-Amz-Signature"))
			}
		})
	}
}
