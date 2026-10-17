package download_test

import (
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// TestDownload_StoredGzipIsNotDecoded asserts that a body served with
// Content-Encoding: gzip is returned as stored when no client is supplied, so the
// bytes do not depend on whether a digest is computed.
func TestDownload_StoredGzipIsNotDecoded(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte("hello world, stored gzipped"))
	r.NoError(err)
	r.NoError(zw.Close())
	stored := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(stored)
	}))
	t.Cleanup(srv.Close)

	b, err := download.Download(t.Context(), download.Request{URL: srv.URL + "/stored.gz"})
	r.NoError(err)
	t.Cleanup(func() { _ = b.Close() })
	r.Equal(stored, readBlob(t, b))
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

// TestDownload_StreamsBeforeBodyCompletes asserts that the blob is handed out while
// the server is still sending the body, so nothing is buffered before it is exposed.
func TestDownload_StreamsBeforeBodyCompletes(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-req.Context().Done():
			return
		}
		_, _ = w.Write([]byte("second"))
	}))
	t.Cleanup(srv.Close)
	// Registered after srv.Close so it runs first: the handler must not block Close.
	t.Cleanup(unblock)

	b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()))
	r.NoError(err)
	rc, err := b.ReadCloser()
	r.NoError(err)
	defer func() { _ = rc.Close() }()

	first := make([]byte, len("first"))
	_, err = io.ReadFull(rc, first)
	r.NoError(err)
	r.Equal("first", string(first), "the first bytes must be readable before the server finishes")

	unblock()
	rest, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("second", string(rest))
}

// TestDownload_ReRead asserts that the first read uses the response Download
// opened, that a later read of a GET sends the request, including its headers,
// again, and that a request with side effects is never sent twice.
func TestDownload_ReRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		verb       string
		wantReread bool
	}{
		{name: "GET is sent again", verb: http.MethodGet, wantReread: true},
		{name: "POST is not sent again", verb: http.MethodPost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				hits.Add(1)
				_, _ = io.WriteString(w, req.Header.Get("X-Echo"))
			}))
			t.Cleanup(srv.Close)

			b, err := download.Download(t.Context(), download.Request{
				URL: srv.URL, Verb: tt.verb, Header: map[string][]string{"X-Echo": {"echoed header"}},
			}, download.WithClient(srv.Client()))
			r.NoError(err)
			r.Equal(int32(1), hits.Load())

			r.Equal("echoed header", string(readBlob(t, b)))
			r.Equal(int32(1), hits.Load(), "the first read must use the response Download opened")

			if !tt.wantReread {
				_, err := b.ReadCloser()
				r.ErrorContains(err, "cannot be read again")
				r.Equal(int32(1), hits.Load(), "the request must not be sent again")
				return
			}
			r.Equal("echoed header", string(readBlob(t, b)))
			r.Equal(int32(2), hits.Load(), "a second read must send the request again")
		})
	}
}

func TestDownload_Size(t *testing.T) {
	t.Parallel()
	content := []byte("sized content")

	tests := []struct {
		name    string
		chunked bool
		want    int64
	}{
		{name: "announced length is the size", want: int64(len(content))},
		{name: "chunked body has unknown size", chunked: true, want: blob.SizeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, content, tt.chunked).URL})
			r.NoError(err)
			r.Equal(tt.want, b.Size())
			r.Equal(content, readBlob(t, b))
		})
	}
}

// TestDownload_Verifier asserts that the verifier sees the response header and the
// digests of the bytes read, and that its rejection fails the reader.
func TestDownload_Verifier(t *testing.T) {
	t.Parallel()
	content := []byte("verified content")
	sum := sha256.Sum256(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test", "v")
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	readToEOF := func(rc io.Reader) error {
		_, err := io.ReadAll(rc)
		return err
	}
	// io.ReadFull drops an error that arrives with the last requested byte.
	readKnownSize := func(rc io.Reader) error {
		_, err := io.ReadFull(rc, make([]byte, len(content)))
		return err
	}

	tests := []struct {
		name         string
		pinned       string
		reject       bool
		consume      func(io.Reader) error
		wantReadErr  bool
		wantCloseErr bool
	}{
		{name: "accepted body reads without error", consume: readToEOF},
		{name: "the pinned digest is handed to the verifier", pinned: "sha256:" + hex.EncodeToString(sum[:]), consume: readToEOF},
		{name: "rejection fails the last read and close", reject: true, consume: readToEOF, wantReadErr: true, wantCloseErr: true},
		{name: "rejection at the known size fails close without EOF", reject: true, consume: readKnownSize, wantCloseErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			var gotHeader http.Header
			var gotComputed download.Computed
			verifier := func(header http.Header, computed download.Computed) error {
				gotHeader, gotComputed = header, computed
				if tt.reject {
					return errors.New("rejected")
				}
				return nil
			}
			b, err := download.Download(t.Context(), download.Request{URL: srv.URL},
				download.WithClient(srv.Client()),
				download.WithDigestAlgorithms(download.DigestAlgorithm{Name: "sha256", New: sha256.New}),
				download.WithVerifier(verifier))
			r.NoError(err)
			if tt.pinned != "" {
				b.SetPrecalculatedDigest(tt.pinned)
			}

			rc, err := b.ReadCloser()
			r.NoError(err)
			readErr := tt.consume(rc)
			closeErr := rc.Close()

			if tt.wantReadErr {
				r.ErrorContains(readErr, "rejected")
			} else {
				r.NoError(readErr)
			}
			if tt.wantCloseErr {
				r.ErrorContains(closeErr, "rejected")
			} else {
				r.NoError(closeErr)
			}
			r.Equal("v", gotHeader.Get("X-Test"))
			r.Equal(hex.EncodeToString(sum[:]), gotComputed.Content["sha256"])
			r.Equal(tt.pinned, gotComputed.Pinned)
			r.Equal(gotComputed.Content, gotComputed.Transferred, "undecoded bytes are transferred as read")
		})
	}
}

// compressible is gzip-compressible content and its gzip encoding.
func compressible(t *testing.T) (content, encoded []byte) {
	t.Helper()
	content = bytes.Repeat([]byte("compressible content "), 100)
	return content, gzipBytes(t, content)
}

// serveIdentity serves body for requests not accepting gzip, honouring ranges.
func serveIdentity(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}
}

// serveNegotiated compresses only for requests accepting gzip, like a proxy or CDN.
func serveNegotiated(compressed []byte, coding string, otherwise http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", coding)
			_, _ = w.Write(compressed)
			return
		}
		otherwise(w, r)
	}
}

// serveStored always labels body with coding, like httpd AddEncoding or S3 metadata.
func serveStored(body []byte, coding string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", coding)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}
}

func serveForbidden(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }

// probeRecorder serves requests and records the Accept-Encoding of downloads and
// whether an identity range probe was sent.
type probeRecorder struct {
	mu      sync.Mutex
	accepts []string
	probed  bool
}

func (p *probeRecorder) serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p.mu.Lock()
		if req.Header.Get("Range") != "" {
			p.probed = true
		} else {
			p.accepts = append(p.accepts, req.Header.Get("Accept-Encoding"))
		}
		p.mu.Unlock()
		handler(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (p *probeRecorder) observed() ([]string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepts, p.probed
}

// TestDownload_CompressTransfer covers compression as a transport concern: the blob
// yields what a request accepting no compression would, and an identity range
// probe tells negotiated gzip from a content coding that is part of the content
// and announces the content size.
func TestDownload_CompressTransfer(t *testing.T) {
	t.Parallel()
	content, encoded := compressible(t)
	hexSum := func(b []byte) string {
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}

	tests := []struct {
		name          string
		noCompression bool
		header        map[string][]string
		serve         http.HandlerFunc
		wantAccept    string
		wantProbe     bool
		want          []byte
		wantDecoded   bool
		wantSize      int64
	}{
		{
			name:  "negotiated gzip is decoded and sized by the probe",
			serve: serveNegotiated(encoded, "gzip", serveIdentity(content)), wantAccept: "gzip", wantProbe: true,
			want: content, wantDecoded: true, wantSize: int64(len(content)),
		},
		{
			name: "a probe answered without range support is sized by its Content-Length",
			serve: serveNegotiated(encoded, "gzip", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(content)))
				_, _ = w.Write(content)
			}),
			wantAccept: "gzip", wantProbe: true, want: content, wantDecoded: true, wantSize: int64(len(content)),
		},
		{
			name:  "a failing probe still decodes negotiated gzip with unknown size",
			serve: serveNegotiated(encoded, "gzip", serveForbidden), wantAccept: "gzip", wantProbe: true,
			want: content, wantDecoded: true, wantSize: blob.SizeUnknown,
		},
		{
			name:  "a coding kept for identity requests is part of the content",
			serve: serveStored(encoded, "gzip"), wantAccept: "gzip", wantProbe: true,
			want: encoded, wantSize: int64(len(encoded)),
		},
		{
			name:  "an unknown coding kept for identity requests is part of the content",
			serve: serveStored(encoded, "br"), wantAccept: "gzip", wantProbe: true,
			want: encoded, wantSize: int64(len(encoded)),
		},
		{
			name:  "an uncompressed response needs no probe",
			serve: serveIdentity(content), wantAccept: "gzip",
			want: content, wantSize: int64(len(content)),
		},
		{
			name: "an Accept-Encoding of the request wins", header: map[string][]string{"Accept-Encoding": {"identity"}},
			serve: serveNegotiated(encoded, "gzip", serveIdentity(content)), wantAccept: "identity",
			want: content, wantSize: int64(len(content)),
		},
		{
			name: "without transfer compression the bytes are kept as served", noCompression: true,
			serve: serveStored(encoded, "gzip"), wantAccept: "",
			want: encoded, wantSize: int64(len(encoded)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			var rec probeRecorder
			srv := rec.serve(t, tt.serve)

			var computed download.Computed
			b, err := download.Download(t.Context(),
				download.Request{URL: srv.URL, Header: tt.header, CompressTransfer: !tt.noCompression},
				download.WithDigestAlgorithms(download.DigestAlgorithm{Name: "sha256", New: sha256.New}),
				download.WithVerifier(func(_ http.Header, c download.Computed) error {
					computed = c
					return nil
				}))
			r.NoError(err)
			accepts, probed := rec.observed()
			r.Equal([]string{tt.wantAccept}, accepts)
			r.Equal(tt.wantProbe, probed, "probe sent")
			r.Equal(tt.wantDecoded, b.Decoded())
			r.Equal(tt.wantSize, b.Size())

			r.Equal(tt.want, readBlob(t, b))
			r.Equal(hexSum(tt.want), computed.Content["sha256"])
			wantTransferred := tt.want
			if tt.wantDecoded {
				wantTransferred = encoded
			}
			r.Equal(hexSum(wantTransferred), computed.Transferred["sha256"], "transferred bytes are digested as received")
		})
	}
}

// TestDownload_CompressTransferFailures covers responses that cannot be turned
// into the content and limits that apply to the decoded content.
func TestDownload_CompressTransferFailures(t *testing.T) {
	t.Parallel()
	content, encoded := compressible(t)

	tests := []struct {
		name            string
		serve           http.HandlerFunc
		maxSize         int64
		wantDownloadErr string
		wantReadErr     string
	}{
		{
			name:            "an unknown negotiated coding is rejected",
			serve:           serveNegotiated(encoded, "br", serveIdentity(content)),
			wantDownloadErr: `unsupported content encoding "br"`,
		},
		{
			name:        "corrupt gzip fails the read",
			serve:       serveNegotiated(content, "gzip", serveIdentity(content)),
			wantReadErr: "gzip",
		},
		{
			name:        "a probe announcing the wrong size fails the read",
			serve:       serveNegotiated(encoded, "gzip", serveIdentity(append(bytes.Clone(content), '!'))),
			wantReadErr: fmt.Sprintf("has %d bytes, expected %d", len(content), len(content)+1),
		},
		{
			name:            "probed content above the size limit is rejected up front",
			serve:           serveNegotiated(encoded, "gzip", serveIdentity(content)),
			maxSize:         int64(len(content)) - 1,
			wantDownloadErr: "exceeds maximum allowed size",
		},
		{
			name:        "unprobed content above the size limit fails the read",
			serve:       serveNegotiated(encoded, "gzip", serveForbidden),
			maxSize:     int64(len(content)) - 1,
			wantReadErr: "exceeds maximum allowed size",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			var rec probeRecorder
			srv := rec.serve(t, tt.serve)

			var opts []download.Option
			if tt.maxSize > 0 {
				opts = append(opts, download.WithMaxDownloadSize(tt.maxSize))
			}
			b, err := download.Download(t.Context(), download.Request{URL: srv.URL, CompressTransfer: true}, opts...)
			if tt.wantDownloadErr != "" {
				r.ErrorContains(err, tt.wantDownloadErr)
				return
			}
			r.NoError(err)

			rc, err := b.ReadCloser()
			r.NoError(err)
			_, readErr := io.ReadAll(rc)
			r.ErrorContains(readErr, tt.wantReadErr)
			r.ErrorContains(rc.Close(), tt.wantReadErr)
		})
	}
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(b)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestDownload_ClosedBlobRefusesReads(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, []byte("payload"), false).URL})
	r.NoError(err)
	r.NoError(b.Close())

	_, err = b.ReadCloser()
	r.ErrorContains(err, "is closed")
	r.NoError(b.Close(), "closing twice must not fail")
}

// TestDownload_MaxDownloadSize covers both rejection paths: a response that announces
// its length is rejected by Download, a chunked one only while it is read.
func TestDownload_MaxDownloadSize(t *testing.T) {
	t.Parallel()
	content := []byte("0123456789") // 10 bytes

	tests := []struct {
		name            string
		maxSize         int64
		unset           bool
		chunked         bool
		wantDownloadErr bool
		wantReadErr     bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit", maxSize: 9, wantDownloadErr: true},
		{name: "zero disables the limit", maxSize: 0},
		{name: "negative disables the limit", maxSize: -1},
		{name: "unset means unlimited", unset: true},
		{name: "oversized chunked body is caught while streaming", maxSize: 9, chunked: true, wantReadErr: true},
		{name: "chunked body below the limit", maxSize: 100, chunked: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			var opts []download.Option
			if !tt.unset {
				opts = append(opts, download.WithMaxDownloadSize(tt.maxSize))
			}

			b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, content, tt.chunked).URL}, opts...)
			if tt.wantDownloadErr {
				r.ErrorContains(err, "exceeds maximum allowed size")
				return
			}
			r.NoError(err)

			rc, err := b.ReadCloser()
			r.NoError(err)
			data, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			if tt.wantReadErr {
				r.ErrorContains(readErr, "exceeds maximum allowed size")
				r.ErrorContains(closeErr, "exceeds maximum allowed size")
				return
			}
			r.NoError(readErr)
			r.NoError(closeErr)
			r.Equal(content, data)
		})
	}
}
