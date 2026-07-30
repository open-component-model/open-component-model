package http_test

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

type logCapture struct {
	records []slog.Record
}

func (l *logCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (l *logCapture) Handle(_ context.Context, r slog.Record) error {
	l.records = append(l.records, r)
	return nil
}

func (l *logCapture) WithAttrs(_ []slog.Attr) slog.Handler { return l }
func (l *logCapture) WithGroup(_ string) slog.Handler      { return l }

func (l *logCapture) warnMessages() []string {
	msgs := make([]string, 0, len(l.records))
	for _, r := range l.records {
		if r.Level == slog.LevelWarn {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}

func TestInsecureWarnTransport(t *testing.T) {
	tr := true

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	t.Cleanup(srv.Close)

	capture := &logCapture{}
	origLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	cfg := &httpv1alpha1.Config{
		TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &tr},
	}
	c := ocmhttp.NewClient(cfg)

	t.Run("first request to host emits WarnContext", func(t *testing.T) {
		before := len(capture.records)
		resp, err := c.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Greater(t, len(capture.records), before)
		assert.NotEmpty(t, capture.warnMessages())
	})

	t.Run("second request to same host does NOT emit another warning", func(t *testing.T) {
		before := len(capture.records)
		resp, err := c.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, before, len(capture.records), "sync.Once must suppress duplicate warnings")
	})
}

func TestNewTransportWithTLS_ConstructionWarning(t *testing.T) {
	tr := true

	capture := &logCapture{}
	origLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	_ = ocmhttp.NewTransportWithTLS(nil, &httpv1alpha1.TLSConfig{InsecureSkipVerify: &tr})

	msgs := capture.warnMessages()
	require.NotEmpty(t, msgs)
	assert.Contains(t, msgs[0], "InsecureSkipVerify=true")
}

func TestNewTransportWithTLS_NoWarningWhenDisabled(t *testing.T) {
	fa := false

	capture := &logCapture{}
	origLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	_ = ocmhttp.NewTransportWithTLS(nil, &httpv1alpha1.TLSConfig{InsecureSkipVerify: &fa})

	assert.Empty(t, capture.warnMessages())
}
