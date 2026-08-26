package integration_test

import (
	"context"
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
