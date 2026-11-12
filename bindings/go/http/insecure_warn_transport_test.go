package http_test

import (
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/internal/testutil/logtest"
)

func TestInsecureWarnTransport(t *testing.T) {
	tr := true

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	t.Cleanup(srv.Close)

	capture := logtest.Default(t, slog.LevelWarn)

	cfg := &httpv1alpha1.Config{
		TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &tr},
	}
	c := ocmhttp.NewClient(cfg)

	t.Run("first request to host emits WarnContext", func(t *testing.T) {
		before := len(capture.Records())
		req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		resp, err := c.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Greater(t, len(capture.Records()), before)
		assert.NotEmpty(t, capture.Messages())
	})

	t.Run("second request to same host does NOT emit another warning", func(t *testing.T) {
		before := len(capture.Records())
		req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		resp, err := c.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Len(t, capture.Records(), before, "sync.Once must suppress duplicate warnings")
	})
}

func TestNewTransportWithTLS_ConstructionWarning(t *testing.T) {
	tr := true

	capture := logtest.Default(t, slog.LevelWarn)

	_, _ = ocmhttp.NewTransportWithTLS(nil, &httpv1alpha1.TLSConfig{InsecureSkipVerify: &tr})

	msgs := capture.Messages()
	require.NotEmpty(t, msgs)
	assert.Contains(t, msgs[0], "InsecureSkipVerify=true")
}

func TestNewTransportWithTLS_NoWarningWhenDisabled(t *testing.T) {
	fa := false

	capture := logtest.Default(t, slog.LevelWarn)

	_, _ = ocmhttp.NewTransportWithTLS(nil, &httpv1alpha1.TLSConfig{InsecureSkipVerify: &fa})

	assert.Empty(t, capture.Messages())
}
