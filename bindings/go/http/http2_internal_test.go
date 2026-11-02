package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// testTLS returns a server TLS config and the matching certificate, taken from
// an httptest server so the certificate is valid for 127.0.0.1.
func testTLS(t *testing.T) (*tls.Config, *x509.Certificate) {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	cfg, cert := ts.TLS.Clone(), ts.Certificate()
	ts.Close()
	return cfg, cert
}

func trustConfig(cert *x509.Certificate) httpv1alpha1.TLSConfig {
	return httpv1alpha1.TLSConfig{
		RootCAsPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})),
	}
}

// rawH2Server is a minimal HTTP/2 server speaking frames directly, so tests can
// inject connection-level events net/http's server never produces on demand.
type rawH2Server struct {
	addr  string
	conns atomic.Int32

	mu       sync.Mutex
	received [][]byte // bodies of fully received PUT requests
}

type rawH2Mode int

const (
	// goawayOnFirstPut: on connection #1, answer the first DATA frame of a PUT
	// with a graceful GOAWAY whose LastStreamID excludes that stream, as a
	// draining front-end does. Later connections behave normally.
	goawayOnFirstPut rawH2Mode = iota
	// silent: complete the handshake, then never answer anything again
	// (including PINGs), like a connection black-holed by a middlebox.
	silent
)

func startRawH2Server(t *testing.T, mode rawH2Mode) *rawH2Server {
	t.Helper()
	cfg, _ := testTLS(t)
	cfg.NextProtos = []string{"h2"}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	require.NoError(t, err)
	s := &rawH2Server{addr: ln.Addr().String()}
	var (
		wg       sync.WaitGroup
		connsMu  sync.Mutex
		accepted []net.Conn
	)
	t.Cleanup(func() {
		_ = ln.Close()
		connsMu.Lock()
		for _, c := range accepted {
			_ = c.Close()
		}
		connsMu.Unlock()
		wg.Wait()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			connsMu.Lock()
			accepted = append(accepted, c)
			connsMu.Unlock()
			n := s.conns.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.serve(c, mode, n == 1)
			}()
		}
	}()
	return s
}

func (s *rawH2Server) bodies() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.received...)
}

func (s *rawH2Server) serve(c net.Conn, mode rawH2Mode, first bool) {
	defer c.Close()
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(c, preface); err != nil {
		return
	}
	fr := http2.NewFramer(c, c)
	fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	if err := fr.WriteSettings(); err != nil {
		return
	}
	var hbuf bytes.Buffer
	enc := hpack.NewEncoder(&hbuf)
	respond := func(id uint32, status string) {
		hbuf.Reset()
		_ = enc.WriteField(hpack.HeaderField{Name: ":status", Value: status})
		_ = enc.WriteField(hpack.HeaderField{Name: "content-length", Value: "0"})
		_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: hbuf.Bytes(), EndStream: true, EndHeaders: true})
	}
	bodies := map[uint32]*bytes.Buffer{}
	goneAway := false
	for {
		f, err := fr.ReadFrame()
		if err != nil {
			return
		}
		if mode == silent {
			if sf, ok := f.(*http2.SettingsFrame); ok && !sf.IsAck() {
				_ = fr.WriteSettingsAck()
			}
			continue
		}
		switch f := f.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				_ = fr.WriteSettingsAck()
			}
		case *http2.PingFrame:
			if !f.IsAck() {
				_ = fr.WritePing(true, f.Data)
			}
		case *http2.MetaHeadersFrame:
			if f.PseudoValue("method") != nethttp.MethodPut {
				respond(f.StreamID, "404")
				continue
			}
			bodies[f.StreamID] = &bytes.Buffer{}
		case *http2.DataFrame:
			body := bodies[f.StreamID]
			if n := len(f.Data()); n > 0 {
				_ = fr.WriteWindowUpdate(0, uint32(n))
				_ = fr.WriteWindowUpdate(f.StreamID, uint32(n))
			}
			if body == nil || goneAway {
				continue
			}
			if first {
				goneAway = true
				var lastStreamID uint32 // the PUT stream and anything after it is unprocessed
				if f.StreamID > 2 {
					lastStreamID = f.StreamID - 2
				}
				_ = fr.WriteGoAway(lastStreamID, http2.ErrCodeNo, nil)
				continue
			}
			body.Write(f.Data())
			if f.StreamEnded() {
				s.mu.Lock()
				s.received = append(s.received, body.Bytes())
				s.mu.Unlock()
				respond(f.StreamID, "201")
			}
		}
	}
}

// closeCounter wraps a seekable reader as a real io.ReadCloser (like *os.File
// or an fs.File) and counts how often it is closed.
type closeCounter struct {
	io.ReadSeeker
	closed atomic.Int32
}

func (c *closeCounter) Close() error {
	c.closed.Add(1)
	return nil
}

// TestNew_ReplaysSeekableBodyOnGOAWAY covers the monolithic blob upload of
// oras-go: it builds the PUT from the caller's reader, so Request.GetBody is
// nil for anything but the stdlib in-memory readers. A graceful GOAWAY that
// arrives after the first body bytes were sent must not fail the upload when
// the body can be rewound.
func TestNew_ReplaysSeekableBodyOnGOAWAY(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 256<<10/16) // 256 KiB, larger than one h2 window
	path := filepath.Join(t.TempDir(), "blob")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	_, cert := testTLS(t)

	open := func(t *testing.T) *os.File {
		t.Helper()
		f, err := os.Open(path)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		return f
	}

	cases := []struct {
		name string
		body func(t *testing.T) io.Reader
	}{
		{"os.File", func(t *testing.T) io.Reader {
			t.Helper()
			return open(t)
		}},
		{"io.NopCloser chain over io.SectionReader", func(t *testing.T) io.Reader {
			t.Helper()
			return io.NopCloser(io.NopCloser(io.NewSectionReader(open(t), 0, int64(len(data)))))
		}},
		{"seekable io.ReadCloser", func(t *testing.T) io.Reader {
			t.Helper()
			return &closeCounter{ReadSeeker: bytes.NewReader(data)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			srv := startRawH2Server(t, goawayOnFirstPut)
			client := New(WithConfig(&httpv1alpha1.Config{TLSConfig: trustConfig(cert)}))

			body := tc.body(t)
			req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPut, "https://"+srv.addr+"/v2/repo/blobs/uploads/u?digest=x", body)
			r.NoError(err)
			req.ContentLength = int64(len(data))
			r.Nil(req.GetBody, "precondition: the request must not be replayable on its own")

			resp, err := client.Do(req)
			r.NoError(err)
			r.NoError(resp.Body.Close())
			r.Equal(nethttp.StatusCreated, resp.StatusCode)
			r.Equal(int32(2), srv.conns.Load(), "upload must be replayed on a fresh connection")
			r.Equal([][]byte{data}, srv.bodies(), "the replayed body must start at the original offset")
			if cc, ok := body.(*closeCounter); ok {
				r.Eventually(func() bool { return cc.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
					"the transport owns req.Body and must close the original exactly once")
			}
		})
	}

	t.Run("non-seekable body still fails", func(t *testing.T) {
		r := require.New(t)
		srv := startRawH2Server(t, goawayOnFirstPut)
		client := New(WithConfig(&httpv1alpha1.Config{TLSConfig: trustConfig(cert)}))
		req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPut, "https://"+srv.addr+"/v2/repo/blobs/uploads/u", struct{ io.Reader }{bytes.NewReader(data)})
		r.NoError(err)
		req.ContentLength = int64(len(data))
		_, err = client.Do(req)
		r.ErrorContains(err, "GOAWAY")
	})
}

// TestNewTransport_DetectsDeadHTTP2Connection asserts that a connection that
// silently stops answering is torn down by HTTP/2 health-check pings instead of
// hanging the request until the TCP stack gives up.
func TestNewTransport_DetectsDeadHTTP2Connection(t *testing.T) {
	r := require.New(t)
	prevSend, prevPing := http2SendPingTimeout, http2PingTimeout
	http2SendPingTimeout, http2PingTimeout = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { http2SendPingTimeout, http2PingTimeout = prevSend, prevPing })

	srv := startRawH2Server(t, silent)
	_, cert := testTLS(t)
	tlsCfg := trustConfig(cert)
	tr, err := NewTransportWithTLS(nil, &tlsCfg)
	r.NoError(err)
	t.Cleanup(tr.CloseIdleConnections)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, "https://"+srv.addr+"/v2/", nil)
	r.NoError(err)
	_, err = (&nethttp.Client{Transport: tr}).Do(req)
	r.Error(err)
	r.NotErrorIs(err, context.DeadlineExceeded, "dead connection must be detected by ping, not by the caller's deadline")
	r.Contains(err.Error(), "connection lost")
}

// TestNew_DisableHTTP2 asserts that disableHTTP2 forces HTTP/1.1 globally or for
// a single host while other hosts keep negotiating HTTP/2.
func TestNew_DisableHTTP2(t *testing.T) {
	newServer := func(t *testing.T) (*httptest.Server, *atomic.Value) {
		t.Helper()
		var proto atomic.Value
		ts := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			proto.Store(r.Proto)
		}))
		ts.EnableHTTP2 = true
		ts.StartTLS()
		t.Cleanup(ts.Close)
		return ts, &proto
	}
	get := func(t *testing.T, c *nethttp.Client, url string) {
		t.Helper()
		req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := c.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	disabled := true

	t.Run("default negotiates HTTP/2", func(t *testing.T) {
		ts, proto := newServer(t)
		get(t, New(WithConfig(&httpv1alpha1.Config{TLSConfig: trustConfig(ts.Certificate())})), ts.URL)
		require.Equal(t, "HTTP/2.0", proto.Load())
	})

	t.Run("global disableHTTP2 forces HTTP/1.1", func(t *testing.T) {
		ts, proto := newServer(t)
		cfg := &httpv1alpha1.Config{
			TLSConfig:      trustConfig(ts.Certificate()),
			ProtocolConfig: httpv1alpha1.ProtocolConfig{DisableHTTP2: &disabled},
		}
		get(t, New(WithConfig(cfg)), ts.URL)
		require.Equal(t, "HTTP/1.1", proto.Load())
	})

	t.Run("per-host disableHTTP2 only affects that host", func(t *testing.T) {
		h1, h1Proto := newServer(t)
		h2, h2Proto := newServer(t)
		pool := httpv1alpha1.TLSConfig{RootCAsPEM: trustConfig(h1.Certificate()).RootCAsPEM + trustConfig(h2.Certificate()).RootCAsPEM}
		cfg := &httpv1alpha1.Config{
			TLSConfig: pool,
			Hosts: map[string]*httpv1alpha1.HostConfig{
				strings.TrimPrefix(h1.URL, "https://"): {ProtocolConfig: httpv1alpha1.ProtocolConfig{DisableHTTP2: &disabled}},
			},
		}
		c := New(WithConfig(cfg))
		get(t, c, h1.URL)
		get(t, c, h2.URL)
		require.Equal(t, "HTTP/1.1", h1Proto.Load())
		require.Equal(t, "HTTP/2.0", h2Proto.Load())
	})
}
