package download

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

func anonymous() Option {
	return WithCredentials(&credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, Anonymous: true})
}

func headResponse(req *http.Request, status int, header http.Header) *http.Response {
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: req}
}

func TestHead_Checksum(t *testing.T) {
	content := []byte("content")
	sum256, sum512 := sha256.Sum256(content), sha512.Sum512(content)
	algorithms := []struct {
		header string
		sum    []byte
		field  func(*ObjectInfo) string
	}{
		{header: "X-Amz-Checksum-Sha256", sum: sum256[:], field: func(i *ObjectInfo) string { return i.SHA256 }},
		{header: "X-Amz-Checksum-Sha512", sum: sum512[:], field: func(i *ObjectInfo) string { return i.SHA512 }},
	}

	for _, algorithm := range algorithms {
		full := base64.StdEncoding.EncodeToString(algorithm.sum)
		want := hex.EncodeToString(algorithm.sum)
		tests := []struct {
			name, checksum, checksumType, want string
		}{
			{name: "full-object checksum", checksum: full, checksumType: "FULL_OBJECT", want: want},
			{name: "store reporting no checksum type", checksum: full, want: want},
			{name: "composite checksum of a multipart upload", checksum: full, checksumType: "COMPOSITE"},
			{name: "composite value without a checksum type", checksum: full + "-3"},
			{name: "no checksum"},
			{name: "value of the wrong length", checksum: base64.StdEncoding.EncodeToString(algorithm.sum[:16]), checksumType: "FULL_OBJECT"},
		}
		for _, tt := range tests {
			t.Run(algorithm.header+"/"+tt.name, func(t *testing.T) {
				r := require.New(t)
				withoutAWSEnvironment(t)
				var requests []*http.Request
				client := &http.Client{Transport: regionTransport(func(req *http.Request) (*http.Response, error) {
					requests = append(requests, req)
					header := http.Header{"X-Amz-Version-Id": {"v-1"}}
					if tt.checksum != "" {
						header.Set(algorithm.header, tt.checksum)
					}
					if tt.checksumType != "" {
						header.Set("X-Amz-Checksum-Type", tt.checksumType)
					}
					return headResponse(req, http.StatusOK, header), nil
				})}

				info, err := Head(t.Context(), Request{BucketName: "b", ObjectKey: "k", Version: "v-1", Endpoint: "https://store.example", UsePathStyle: true},
					WithHTTPClient(client), anonymous())
				r.NoError(err)
				r.Equal(tt.want, algorithm.field(info))
				r.Equal("v-1", info.VersionID)

				r.Len(requests, 1)
				r.Equal(http.MethodHead, requests[0].Method)
				r.Equal("/b/k", requests[0].URL.Path)
				r.Equal("v-1", requests[0].URL.Query().Get("versionId"))
				r.Equal("ENABLED", requests[0].Header.Get("X-Amz-Checksum-Mode"), "S3 reports checksums on HEAD only when asked")
			})
		}
	}
}

// A HEAD carries no error body, so S3's 301 reaches the SDK as MovedPermanently rather
// than PermanentRedirect; it must still lead to the region correction a GET gets.
func TestHead_RegionCorrection(t *testing.T) {
	r := require.New(t)
	withoutAWSEnvironment(t)
	var hosts []string
	client := &http.Client{Transport: regionTransport(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		if len(hosts) == 1 {
			return headResponse(req, http.StatusMovedPermanently, http.Header{"X-Amz-Bucket-Region": {"eu-west-1"}}), nil
		}
		return headResponse(req, http.StatusOK, http.Header{"X-Amz-Version-Id": {"v-1"}}), nil
	})}

	info, err := Head(t.Context(), Request{Region: "us-east-1", BucketName: "test-bucket", ObjectKey: "object"}, WithHTTPClient(client), anonymous())
	r.NoError(err)
	r.Equal("v-1", info.VersionID)
	r.Equal([]string{"test-bucket.s3.us-east-1.amazonaws.com", "test-bucket.s3.eu-west-1.amazonaws.com"}, hosts)
}

func TestHead_RequiresObject(t *testing.T) {
	_, err := Head(t.Context(), Request{BucketName: "b"}, anonymous())
	require.ErrorContains(t, err, "objectKey is required")
}
