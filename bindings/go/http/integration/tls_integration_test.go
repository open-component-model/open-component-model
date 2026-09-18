package integration_test

import (
	"context"
	"encoding/pem"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

type tlsLogCapture struct {
	records []slog.Record
}

func (l *tlsLogCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (l *tlsLogCapture) Handle(_ context.Context, r slog.Record) error {
	l.records = append(l.records, r)
	return nil
}

func (l *tlsLogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return l }
func (l *tlsLogCapture) WithGroup(_ string) slog.Handler      { return l }

func (l *tlsLogCapture) warnMessages() []string {
	msgs := make([]string, 0, len(l.records))
	for _, r := range l.records {
		if r.Level == slog.LevelWarn {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}

func TestTLSInsecureSkipVerify_Integration(t *testing.T) {
	tr := true

	srv := httptest.NewTLSServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	t.Cleanup(srv.Close)

	t.Run("default client fails with self-signed cert", func(t *testing.T) {
		c := ocmhttp.NewClient(nil)
		_, err := c.Get(srv.URL)
		require.Error(t, err, "client without InsecureSkipVerify must reject self-signed cert")
		assert.Contains(t, err.Error(), "certificate")
	})

	t.Run("InsecureSkipVerify=true succeeds and emits warning", func(t *testing.T) {
		capture := &tlsLogCapture{}
		origLogger := slog.Default()
		slog.SetDefault(slog.New(capture))
		t.Cleanup(func() { slog.SetDefault(origLogger) })

		cfg := &httpv1alpha1.Config{
			TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &tr},
		}
		c := ocmhttp.NewClient(cfg)
		resp, err := c.Get(srv.URL)
		require.NoError(t, err, "client with InsecureSkipVerify=true must succeed against self-signed cert")
		resp.Body.Close()
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)

		msgs := capture.warnMessages()
		assert.NotEmpty(t, msgs, "warning must be emitted when InsecureSkipVerify=true")
		assert.Contains(t, msgs[0], "InsecureSkipVerify=true", "warning must identify the specific setting")
	})
}

// TestTLSRootCAs_Integration proves that a client configured with RootCAsPEM
// trusts a server whose certificate chains to that CA, without disabling
// verification. The httptest TLS server's own certificate acts as the CA here:
// a client that pins it as a root CA succeeds where a default client fails.
func TestTLSRootCAs_Integration(t *testing.T) {
	srv := httptest.NewTLSServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	t.Cleanup(srv.Close)

	caPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: srv.Certificate().Raw,
	}))

	t.Run("RootCAsPEM lets a verifying client trust the server cert", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: caPEM},
		}
		c := ocmhttp.NewClient(cfg)
		resp, err := c.Get(srv.URL)
		require.NoError(t, err, "client with the server CA in RootCAsPEM must verify successfully")
		resp.Body.Close()
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)
	})

	t.Run("invalid RootCAsPEM fails the request closed", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: "not a certificate"},
		}
		c := ocmhttp.NewClient(cfg)
		_, err := c.Get(srv.URL)
		require.Error(t, err, "an unparseable RootCAsPEM must fail the request, not fall back to system trust")
		assert.Contains(t, err.Error(), "no valid certificates")
	})

	t.Run("InsecureSkipVerify overrides RootCAs and skips verification", func(t *testing.T) {
		tr := true
		cfg := &httpv1alpha1.Config{
			TLSConfig: httpv1alpha1.TLSConfig{
				InsecureSkipVerify: &tr,
				RootCAsPEM:         "not a certificate",
			},
		}
		c := ocmhttp.NewClient(cfg)
		resp, err := c.Get(srv.URL)
		require.NoError(t, err, "InsecureSkipVerify=true must skip CA loading entirely")
		resp.Body.Close()
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)
	})
}
