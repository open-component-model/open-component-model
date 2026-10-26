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
// Content-Encoding: gzip is returned as stored, also with an injected client that
// would transparently decompress, so sizes and checksums match the served bytes.
func TestDownload_StoredGzipIsNotDecoded(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte("hello world, stored gzipped"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	stored := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(stored)
	}))
	t.Cleanup(srv.Close)

	for name, opts := range map[string][]download.Option{
		"default client":       nil,
		"decompressing client": {download.WithClient(&http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()})},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			b, err := download.Download(t.Context(), download.Request{URL: srv.URL + "/stored.gz"}, opts...)
			r.NoError(err)
			r.Equal(stored, readBlob(t, b))
		})
	}
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
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL + "/redirect", NoRedirect: true},
			download.WithClient(srv.Client()))
		require.NoError(t, err)
		rc, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.ErrorContains(t, err, "status 302")
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
// while streaming rather than up front. Otherwise the length is announced, also
// for HEAD requests.
func serveBody(t *testing.T, content []byte, chunked bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if chunked {
			w.(http.Flusher).Flush()
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
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

// TestDownload_ReRead asserts that Download sends nothing, that every read of a
// GET sends the request, including its headers, and that a request with side
// effects is never sent twice.
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
			r.Equal(int32(0), hits.Load(), "Download must not send the request")

			r.Equal("echoed header", string(readBlob(t, b)))
			r.Equal(int32(1), hits.Load())

			if !tt.wantReread {
				rc, err := b.ReadCloser()
				r.NoError(err)
				_, err = io.ReadAll(rc)
				r.ErrorContains(err, "cannot be read again")
				r.NoError(rc.Close())
				r.Equal(int32(1), hits.Load(), "the request must not be sent again")
				return
			}
			r.Equal("echoed header", string(readBlob(t, b)))
			r.Equal(int32(2), hits.Load(), "a second read must send the request again")
		})
	}
}

// TestDownload_UnreadReaderSendsNothing asserts that a reader closed before its
// first Read sends no request, so a caller that gives up before reading (such as
// a push falling back to another path) can still read a POST download once.
func TestDownload_UnreadReaderSendsNothing(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "posted")
	}))
	t.Cleanup(srv.Close)

	b, err := download.Download(t.Context(), download.Request{URL: srv.URL, Verb: http.MethodPost},
		download.WithClient(srv.Client()))
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	r.NoError(rc.Close())
	r.Equal(int32(0), hits.Load(), "an unread reader must not send the request")

	r.Equal("posted", string(readBlob(t, b)))
	r.Equal(int32(1), hits.Load())
}

// TestDownload_Metadata asserts that size, media type and advertised digest are
// learned once, from a HEAD request when asked for before any read, or from the
// first response, and that requests with side effects are not probed.
func TestDownload_Metadata(t *testing.T) {
	t.Parallel()
	content := []byte("metadata content")
	advertised := download.WithAdvertisedDigest(func(header http.Header) (string, bool) {
		d := header.Get("X-Digest")
		return d, d != ""
	})
	newServer := func(t *testing.T, heads, gets *atomic.Int32) *httptest.Server {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodHead {
				heads.Add(1)
			} else {
				gets.Add(1)
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Digest", "sha256:advertised")
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	requireMetadata := func(r *require.Assertions, b *download.Blob) {
		r.Equal(int64(len(content)), b.Size())
		mediaType, _ := b.MediaType()
		r.Equal("text/plain", mediaType)
		dig, ok := b.Digest()
		r.True(ok)
		r.Equal("sha256:advertised", dig)
	}

	t.Run("asked before reading", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		var heads, gets atomic.Int32
		srv := newServer(t, &heads, &gets)
		var pinned string
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()), advertised,
			download.WithVerifier(func(_ http.Header, c download.Computed) error {
				pinned = c.Pinned
				return nil
			}))
		r.NoError(err)

		requireMetadata(r, b)
		requireMetadata(r, b)
		r.Equal(int32(1), heads.Load(), "metadata must be requested once")
		r.Equal(int32(0), gets.Load(), "metadata must not send the request")

		r.Equal(content, readBlob(t, b))
		r.Equal("sha256:advertised", pinned, "the reported digest must be verified")
		r.Equal(int32(1), heads.Load())
	})

	t.Run("learned from the first response", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		var heads, gets atomic.Int32
		srv := newServer(t, &heads, &gets)
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()), advertised)
		r.NoError(err)

		r.Equal(content, readBlob(t, b))
		requireMetadata(r, b)
		r.Equal(int32(0), heads.Load(), "a read response must make the HEAD request unnecessary")
	})

	t.Run("request with side effects is not probed", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		var heads, gets atomic.Int32
		srv := newServer(t, &heads, &gets)
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL, Verb: http.MethodPost}, download.WithClient(srv.Client()), advertised)
		r.NoError(err)

		r.Equal(blob.SizeUnknown, b.Size())
		_, ok := b.Digest()
		r.False(ok)
		r.Equal(int32(0), heads.Load()+gets.Load(), "no request may be sent")
		r.Equal(content, readBlob(t, b))
	})

	t.Run("failing HEAD leaves metadata unknown", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodHead {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()))
		r.NoError(err)

		r.Equal(blob.SizeUnknown, b.Size())
		r.Equal(content, readBlob(t, b))
		r.Equal(blob.SizeUnknown, b.Size(), "metadata must not change once learned")
	})

	t.Run("response of another size than announced by HEAD is rejected", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodHead {
				w.Header().Set("Content-Length", "3")
				return
			}
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)
		b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()))
		r.NoError(err)

		r.Equal(int64(3), b.Size())
		rc, err := b.ReadCloser()
		r.NoError(err)
		_, err = io.ReadAll(rc)
		r.ErrorContains(err, "expected 3")
		r.NoError(rc.Close())
	})
}

// TestDownload_HeaderCheck asserts that a rejected response header fails the
// reader before the body is read.
func TestDownload_HeaderCheck(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var verified bool
	b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, []byte("payload"), false).URL},
		download.WithHeaderCheck(func(http.Header) error { return errors.New("header rejected") }),
		download.WithVerifier(func(http.Header, download.Computed) error {
			verified = true
			return nil
		}))
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	n, err := rc.Read(make([]byte, 16))
	r.ErrorContains(err, "header rejected")
	r.Zero(n)
	r.NoError(rc.Close())
	r.False(verified, "the body must not be read")
}

// TestDownload_RereadWithoutLengthIsCheckedAgainstKnownSize asserts that a
// re-read whose response announces no length is read to its end and checked
// against the learned size, instead of being cut at that size.
func TestDownload_RereadWithoutLengthIsCheckedAgainstKnownSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reread  string
		wantErr bool
	}{
		{name: "same length is accepted", reread: "xyz"},
		{name: "longer body is rejected", reread: "xyzdef", wantErr: true},
		{name: "shorter body is rejected", reread: "x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			var gets atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodHead || gets.Add(1) == 1 {
					_, _ = io.WriteString(w, "abc")
					return
				}
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, tt.reread)
			}))
			t.Cleanup(srv.Close)

			b, err := download.Download(t.Context(), download.Request{URL: srv.URL}, download.WithClient(srv.Client()))
			r.NoError(err)
			r.Equal(int64(3), b.Size())
			r.Equal("abc", string(readBlob(t, b)))

			// Stop at the known size like a consumer copying exactly Size bytes.
			rc, err := b.ReadCloser()
			r.NoError(err)
			got := make([]byte, 3)
			n, readErr := io.ReadFull(rc, got)
			closeErr := rc.Close()
			if !tt.wantErr {
				r.NoError(readErr)
				r.NoError(closeErr)
				r.Equal(tt.reread, string(got[:n]))
				return
			}
			r.ErrorContains(errors.Join(readErr, closeErr), "does not have the expected 3 bytes")
		})
	}
}

// TestDownload_EmptyContentIsVerifiedOnClose asserts that empty content is
// verified even when the consumer, knowing the size is zero, never reads.
func TestDownload_EmptyContentIsVerifiedOnClose(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, nil, false).URL},
		download.WithVerifier(func(http.Header, download.Computed) error { return errors.New("rejected") }))
	r.NoError(err)
	r.Equal(int64(0), b.Size())

	rc, err := b.ReadCloser()
	r.NoError(err)
	r.ErrorContains(rc.Close(), "rejected")
}

// TestDownload_PartlyReadContentIsVerifiedOnClose asserts that a consumer
// stopping before the end of the body cannot skip the verifier.
func TestDownload_PartlyReadContentIsVerifiedOnClose(t *testing.T) {
	t.Parallel()
	content := []byte("partly read content")

	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked=%t", chunked), func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			var computed download.Computed
			b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, content, chunked).URL},
				download.WithDigestAlgorithms(download.DigestAlgorithm{Name: "sha256", New: sha256.New}),
				download.WithVerifier(func(_ http.Header, c download.Computed) error {
					computed = c
					return errors.New("rejected")
				}))
			r.NoError(err)

			rc, err := b.ReadCloser()
			r.NoError(err)
			_, err = io.ReadFull(rc, make([]byte, 5))
			r.NoError(err)
			r.ErrorContains(rc.Close(), "rejected")
			sum := sha256.Sum256(content)
			r.Equal(hex.EncodeToString(sum[:]), computed.Content["sha256"], "the verifier must see the whole body")
		})
	}
}

// TestDownload_AbandonedLargeBodyIsNotDrainedOnClose asserts that Close does not
// download a large unread rest of the body to verify it, but reports the content
// as not verified instead.
func TestDownload_AbandonedLargeBodyIsNotDrainedOnClose(t *testing.T) {
	t.Parallel()
	content := bytes.Repeat([]byte("x"), 4<<20)

	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked=%t", chunked), func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			var verified bool
			b, err := download.Download(t.Context(), download.Request{URL: serveBody(t, content, chunked).URL},
				download.WithVerifier(func(http.Header, download.Computed) error {
					verified = true
					return nil
				}))
			r.NoError(err)

			rc, err := b.ReadCloser()
			r.NoError(err)
			_, err = io.ReadFull(rc, make([]byte, 5))
			r.NoError(err)
			r.ErrorContains(rc.Close(), "content not verified")
			r.False(verified, "the rest of the body must not be read")
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
		})
	}
}

// TestDownload_MaxDownloadSize covers both rejection paths: a response that
// announces its length is rejected before its body is read, a chunked one while
// it is read.
func TestDownload_MaxDownloadSize(t *testing.T) {
	t.Parallel()
	content := []byte("0123456789") // 10 bytes

	tests := []struct {
		name         string
		maxSize      int64
		unset        bool
		chunked      bool
		wantReadErr  bool
		wantCloseErr bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit", maxSize: 9, wantReadErr: true},
		{name: "zero disables the limit", maxSize: 0},
		{name: "negative disables the limit", maxSize: -1},
		{name: "unset means unlimited", unset: true},
		{name: "oversized chunked body is caught while streaming", maxSize: 9, chunked: true, wantReadErr: true, wantCloseErr: true},
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
			r.NoError(err)

			rc, err := b.ReadCloser()
			r.NoError(err)
			data, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			if tt.wantCloseErr {
				r.ErrorContains(closeErr, "exceeds maximum allowed size")
			} else {
				r.NoError(closeErr)
			}
			if tt.wantReadErr {
				r.ErrorContains(readErr, "exceeds maximum allowed size")
				return
			}
			r.NoError(readErr)
			r.Equal(content, data)
		})
	}
}
