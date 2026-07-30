package download_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

// readBlob fully reads a blob and returns its bytes.
func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

// wgetCreds builds a typed WgetCredentials value.
func wgetCreds(c credv1.WgetCredentials) *credv1.WgetCredentials {
	c.Type = credv1.WgetCredentialsVersionedType
	return &c
}

// echoAuthServer echoes the authentication seen by the server: "basic:<user>:<pass>",
// the raw "Bearer <token>" header, or "none".
func echoAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); ok {
			fmt.Fprintf(w, "basic:%s:%s", u, p)
			return
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			fmt.Fprint(w, auth)
			return
		}
		fmt.Fprint(w, "none")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownload_HappyPath(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()))
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, content, readBlob(t, b))

	got, _ := b.MediaType()
	assert.Equal(t, "text/plain", got)
}

func TestDownload_Credentials(t *testing.T) {
	t.Parallel()

	t.Run("no credentials send no Authorization", func(t *testing.T) {
		t.Parallel()
		srv := echoAuthServer(t)
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()))
		require.NoError(t, err)
		assert.Equal(t, "none", string(readBlob(t, b)))
	})

	t.Run("basic auth", func(t *testing.T) {
		t.Parallel()
		srv := echoAuthServer(t)
		creds := wgetCreds(credv1.WgetCredentials{Username: "user", Password: "pass"})
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL},
			download.WithClient(srv.Client()), download.WithCredentials(creds))
		require.NoError(t, err)
		assert.Equal(t, "basic:user:pass", string(readBlob(t, b)))
	})

	t.Run("bearer token", func(t *testing.T) {
		t.Parallel()
		srv := echoAuthServer(t)
		creds := wgetCreds(credv1.WgetCredentials{IdentityToken: "my-token"})
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL},
			download.WithClient(srv.Client()), download.WithCredentials(creds))
		require.NoError(t, err)
		assert.Equal(t, "Bearer my-token", string(readBlob(t, b)))
	})

	t.Run("bearer token takes precedence over basic auth", func(t *testing.T) {
		t.Parallel()
		srv := echoAuthServer(t)
		creds := wgetCreds(credv1.WgetCredentials{Username: "user", Password: "pass", IdentityToken: "my-token"})
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL},
			download.WithClient(srv.Client()), download.WithCredentials(creds))
		require.NoError(t, err)
		assert.Equal(t, "Bearer my-token", string(readBlob(t, b)))
	})
}

func TestDownload_Redirect(t *testing.T) {
	t.Parallel()

	newRedirectServer := func(t *testing.T) *httptest.Server {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/target" {
				_, _ = w.Write([]byte("final"))
				return
			}
			http.Redirect(w, r, "/target", http.StatusFound)
		}))
		t.Cleanup(srv.Close)
		return srv
	}

	t.Run("follows redirects by default", func(t *testing.T) {
		t.Parallel()
		srv := newRedirectServer(t)
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL + "/redirect"}, download.WithClient(srv.Client()))
		require.NoError(t, err)
		assert.Equal(t, "final", string(readBlob(t, b)))
	})

	t.Run("NoRedirect surfaces the redirect status instead of following", func(t *testing.T) {
		t.Parallel()
		srv := newRedirectServer(t)
		_, err := download.Download(t.Context(), download.Request{URL: srv.URL + "/redirect", NoRedirect: true},
			download.WithClient(srv.Client()))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 302")
	})
}

// --- mTLS -------------------------------------------------------------------

// newCA generates a self-signed CA certificate and its private key.
func newCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "wget-download-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key
}

// issueClientCert issues a client-auth leaf certificate signed by the given CA.
func issueClientCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "wget-download-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestDownload_MTLS(t *testing.T) {
	t.Parallel()

	ca, caKey := newCA(t)
	clientCertPEM, clientKeyPEM := issueClientCert(t, ca, caKey)

	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no client certificate", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("mtls-ok:" + r.Header.Get("Authorization")))
	}))
	srv.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  clientCAPool,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	// The client must trust the server's (self-signed) certificate.
	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	t.Run("client certificate authenticates the request", func(t *testing.T) {
		t.Parallel()
		creds := wgetCreds(credv1.WgetCredentials{
			Certificate:          string(clientCertPEM),
			PrivateKey:           string(clientKeyPEM),
			CertificateAuthority: string(serverCertPEM),
		})
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithCredentials(creds))
		require.NoError(t, err)
		assert.Equal(t, "mtls-ok:", string(readBlob(t, b)))
	})

	t.Run("client certificate composes with a bearer token", func(t *testing.T) {
		t.Parallel()
		creds := wgetCreds(credv1.WgetCredentials{
			IdentityToken:        "my-token",
			Certificate:          string(clientCertPEM),
			PrivateKey:           string(clientKeyPEM),
			CertificateAuthority: string(serverCertPEM),
		})
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithCredentials(creds))
		require.NoError(t, err)
		assert.Equal(t, "mtls-ok:Bearer my-token", string(readBlob(t, b)))
	})
}

// serveBody serves content over HTTP. When chunked is set, the response is flushed
// before the body is written, so it is sent with chunked transfer encoding and the
// client sees no Content-Length — which is what forces the size limit to be enforced
// while streaming rather than up front.
func serveBody(t *testing.T, content []byte, chunked bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if chunked {
			w.(http.Flusher).Flush()
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDownload_StreamsIntoTempDir asserts that the body lands in the directory given
// by WithTempDir, which is what lets callers place downloads on a configured volume.
func TestDownload_StreamsIntoTempDir(t *testing.T) {
	tempDir := t.TempDir()
	content := []byte("streamed to disk")

	b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, content, false).URL},
		download.WithTempDir(tempDir))
	require.NoError(t, err)

	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the body must be streamed into the configured directory")

	onDisk, err := os.ReadFile(filepath.Join(tempDir, entries[0].Name()))
	require.NoError(t, err)
	assert.Equal(t, content, onDisk)
	assert.Equal(t, content, readBlob(t, b))
}

// TestDownload_CloseRemovesTempFile asserts that closing the returned blob reclaims
// the temporary file, so a caller that is done with a download leaves nothing behind.
func TestDownload_CloseRemovesTempFile(t *testing.T) {
	tempDir := t.TempDir()

	b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, []byte("payload"), false).URL},
		download.WithTempDir(tempDir))
	require.NoError(t, err)

	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.NoError(t, b.Close())
	entries, err = os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "closing the blob must remove the temporary file")

	assert.NoError(t, b.Close(), "closing twice must not fail")
}

// TestDownload_AbandonedBlobIsReclaimed asserts that a blob dropped without being
// closed still has its temporary file removed, so callers that cannot close it do
// not accumulate downloads for the lifetime of the process.
func TestDownload_AbandonedBlobIsReclaimed(t *testing.T) {
	tempDir := t.TempDir()
	srv := serveBody(t, []byte("payload"), false)

	func() {
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL},
			download.WithTempDir(tempDir))
		require.NoError(t, err)
		require.NotNil(t, b)

		entries, err := os.ReadDir(tempDir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
	}()

	// The cleanup runs asynchronously once the blob is unreachable, so collect and poll.
	require.Eventually(t, func() bool {
		runtime.GC()
		entries, err := os.ReadDir(tempDir)
		return err == nil && len(entries) == 0
	}, 10*time.Second, 20*time.Millisecond, "an abandoned download must not leave its temporary file behind")
}

// TestDownload_MaxDownloadSize covers both rejection paths: a response that announces
// its length is rejected before transfer, a chunked one only while streaming.
func TestDownload_MaxDownloadSize(t *testing.T) {
	content := []byte("0123456789") // 10 bytes

	tests := []struct {
		name    string
		maxSize int64
		unset   bool
		chunked bool
		wantErr bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit", maxSize: 9, wantErr: true},
		{name: "zero disables the limit", maxSize: 0},
		{name: "negative disables the limit", maxSize: -1},
		{name: "unset means unlimited", unset: true},
		{name: "oversized chunked body is caught while streaming", maxSize: 9, chunked: true, wantErr: true},
		{name: "chunked body below the limit", maxSize: 100, chunked: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			opts := []download.Option{download.WithTempDir(tempDir)}
			if !tt.unset {
				opts = append(opts, download.WithMaxDownloadSize(tt.maxSize))
			}

			b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, content, tt.chunked).URL}, opts...)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "exceeds maximum allowed size")

				entries, err := os.ReadDir(tempDir)
				require.NoError(t, err)
				assert.Empty(t, entries, "a rejected download must not leave a partial file behind")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, content, readBlob(t, b))
		})
	}
}
