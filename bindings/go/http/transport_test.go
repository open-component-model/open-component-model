package http_test

import (
	nethttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

var defaultTransport = nethttp.DefaultTransport.(*nethttp.Transport)

// configYAML decodes a full OCM generic config envelope from the given YAML
// and converts its first entry into a typed *httpv1alpha1.Config. It
// deliberately bypasses LookupConfig / ResolveHTTPConfig — those inject
// DefaultTimeout when timeout is omitted — so the resulting Config carries
// exactly what the YAML said, with every unset field left as a nil pointer.
func configYAML(t *testing.T, yaml string) *httpv1alpha1.Config {
	t.Helper()

	var generic genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &generic),
		"decode generic config yaml")
	require.Len(t, generic.Configurations, 1)

	var cfg httpv1alpha1.Config
	require.NoError(t, httpv1alpha1.Scheme.Convert(generic.Configurations[0], &cfg),
		"convert generic entry into http config")
	return &cfg
}

func TestNewTransport(t *testing.T) {
	t.Run("nil cfg preserves DefaultTransport values", func(t *testing.T) {
		tr := ocmhttp.NewTransport(nil)
		assert.Equal(t, defaultTransport.TLSHandshakeTimeout, tr.TLSHandshakeTimeout)
		assert.Equal(t, defaultTransport.IdleConnTimeout, tr.IdleConnTimeout)
		assert.Equal(t, defaultTransport.ResponseHeaderTimeout, tr.ResponseHeaderTimeout)
		assert.Equal(t, defaultTransport.ExpectContinueTimeout, tr.ExpectContinueTimeout)
		assert.Equal(t, defaultTransport.MaxIdleConns, tr.MaxIdleConns)
		assert.Equal(t, defaultTransport.ForceAttemptHTTP2, tr.ForceAttemptHTTP2)
	})

	t.Run("empty cfg preserves DefaultTransport values", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.Equal(t, defaultTransport.TLSHandshakeTimeout, tr.TLSHandshakeTimeout)
		assert.Equal(t, defaultTransport.IdleConnTimeout, tr.IdleConnTimeout)
		assert.Equal(t, defaultTransport.ResponseHeaderTimeout, tr.ResponseHeaderTimeout)
		assert.Equal(t, defaultTransport.ExpectContinueTimeout, tr.ExpectContinueTimeout)
		assert.Equal(t, defaultTransport.MaxIdleConns, tr.MaxIdleConns)
		assert.Equal(t, defaultTransport.ForceAttemptHTTP2, tr.ForceAttemptHTTP2)
	})

	t.Run("overrides TLSHandshakeTimeout only", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    tlsHandshakeTimeout: 5s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.Equal(t, 5*time.Second, tr.TLSHandshakeTimeout)
		assert.Equal(t, defaultTransport.IdleConnTimeout, tr.IdleConnTimeout)
		assert.Equal(t, defaultTransport.ResponseHeaderTimeout, tr.ResponseHeaderTimeout)
	})

	t.Run("overrides IdleConnTimeout only", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    idleConnTimeout: 120s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.Equal(t, 120*time.Second, tr.IdleConnTimeout)
		assert.Equal(t, defaultTransport.TLSHandshakeTimeout, tr.TLSHandshakeTimeout)
	})

	t.Run("overrides ResponseHeaderTimeout only", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    responseHeaderTimeout: 20s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.Equal(t, 20*time.Second, tr.ResponseHeaderTimeout)
		assert.Equal(t, defaultTransport.TLSHandshakeTimeout, tr.TLSHandshakeTimeout)
	})

	t.Run("replaces DialContext when TCPDialTimeout is set", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    tcpDialTimeout: 15s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.NotNil(t, tr.DialContext)
		assert.Equal(t, defaultTransport.TLSHandshakeTimeout, tr.TLSHandshakeTimeout)
	})

	t.Run("replaces DialContext when negative TCPKeepAlive disables probes", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    tcpKeepAlive: -1s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.NotNil(t, tr.DialContext)
	})

	t.Run("zero IdleConnTimeout overrides DefaultTransport's non-zero default", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    idleConnTimeout: 0s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.Zero(t, tr.IdleConnTimeout)
	})

	t.Run("applies all values and preserves non-timeout defaults", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    tcpDialTimeout: 1s
    tcpKeepAlive: 2s
    tlsHandshakeTimeout: 3s
    responseHeaderTimeout: 4s
    idleConnTimeout: 5s
`)
		tr := ocmhttp.NewTransport(&cfg.TimeoutConfig)
		assert.Equal(t, 3*time.Second, tr.TLSHandshakeTimeout)
		assert.Equal(t, 4*time.Second, tr.ResponseHeaderTimeout)
		assert.Equal(t, 5*time.Second, tr.IdleConnTimeout)
		assert.NotNil(t, tr.DialContext)
		assert.Equal(t, defaultTransport.ExpectContinueTimeout, tr.ExpectContinueTimeout)
		assert.Equal(t, defaultTransport.MaxIdleConns, tr.MaxIdleConns)
		assert.Equal(t, defaultTransport.ForceAttemptHTTP2, tr.ForceAttemptHTTP2)
	})
}

func TestNewClient(t *testing.T) {
	t.Run("nil cfg returns client with default transport and no Timeout", func(t *testing.T) {
		c := ocmhttp.NewClient(nil)
		require.NotNil(t, c.Transport)
		assert.Zero(t, c.Timeout)
	})

	t.Run("Timeout flows through to http.Client.Timeout", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 5m
`)
		c := ocmhttp.NewClient(cfg)
		assert.Equal(t, 5*time.Minute, c.Timeout)
	})

	t.Run("zero Timeout pointer disables overall deadline", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 0s
`)
		c := ocmhttp.NewClient(cfg)
		assert.Zero(t, c.Timeout)
	})

	t.Run("nil Timeout leaves http.Client.Timeout unset", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    idleConnTimeout: 60s
`)
		c := ocmhttp.NewClient(cfg)
		assert.Zero(t, c.Timeout)
	})

	t.Run("transport timeouts flow through to http.Client.Transport", func(t *testing.T) {
		cfg := configYAML(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    tlsHandshakeTimeout: 7s
    idleConnTimeout: 90s
`)
		c := ocmhttp.NewClient(cfg)
		tr, ok := c.Transport.(*nethttp.Transport)
		require.True(t, ok, "expected *http.Transport, got %T", c.Transport)
		assert.Equal(t, 7*time.Second, tr.TLSHandshakeTimeout)
		assert.Equal(t, 90*time.Second, tr.IdleConnTimeout)
	})
}
