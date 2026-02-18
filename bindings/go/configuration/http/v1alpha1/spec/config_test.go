package spec_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpspec "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
)

func TestConfig_ParseYAML(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		tests := []struct {
			name   string
			yaml   string
			expect *httpspec.Timeout
		}{
			{
				name: "parses string like 5m",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 5m
`,
				expect: httpspec.NewTimeout(5 * time.Minute),
			},
			{
				name: "parses nanoseconds number",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 300000000000
`,
				expect: httpspec.NewTimeout(5 * time.Minute),
			},
			{
				name: "defaults to nil when omitted",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
`,
				expect: nil,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var generic genericv1.Config
				err := genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic)
				require.NoError(t, err)
				require.Len(t, generic.Configurations, 1)

				var cfg httpspec.Config
				err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
				require.NoError(t, err)

				assert.Equal(t, tt.expect, cfg.Timeout)
			})
		}
	})

	t.Run("invalid", func(t *testing.T) {
		tests := []struct {
			name      string
			yaml      string
			expectMsg string
		}{
			{
				name: "rejects unknown unit like 1Gb",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 1Gb
`,
				expectMsg: `invalid timeout value "1Gb": must be a duration like 30s, 5m, or nanoseconds number`,
			},
			{
				name: "rejects non-duration string",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: notaduration
`,
				expectMsg: `invalid timeout value "notaduration": must be a duration like 30s, 5m, or nanoseconds number`,
			},
			{
				name: "rejects non-string non-number type",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: true
`,
				expectMsg: `timeout must be a duration string or nanoseconds number, got bool`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var generic genericv1.Config
				err := genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic)
				require.NoError(t, err)
				require.Len(t, generic.Configurations, 1)

				var cfg httpspec.Config
				err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectMsg)
			})
		}
	})
}

func TestLookupConfig(t *testing.T) {
	t.Run("defaults applied when no timeouts provided", func(t *testing.T) {
		yamlCfg := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yamlCfg), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.LookupConfig(&generic)
		require.NoError(t, err)

		assert.Equal(t, &httpspec.DefaultTimeout, cfg.Timeout, "Timeout")
		assert.Equal(t, &httpspec.DefaultTCPDialTimeout, cfg.TCPDialTimeout, "TCPDialTimeout")
		assert.Equal(t, &httpspec.DefaultTCPKeepAlive, cfg.TCPKeepAlive, "TCPKeepAlive")
		assert.Equal(t, &httpspec.DefaultTLSHandshakeTimeout, cfg.TLSHandshakeTimeout, "TLSHandshakeTimeout")
		assert.Equal(t, &httpspec.DefaultResponseHeaderTimeout, cfg.ResponseHeaderTimeout, "ResponseHeaderTimeout")
		assert.Equal(t, &httpspec.DefaultIdleConnTimeout, cfg.IdleConnTimeout, "IdleConnTimeout")
	})

	t.Run("exact values used when all timeouts provided", func(t *testing.T) {
		yamlCfg := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 1m
    tcpDialTimeout: 15s
    tcpKeepAlive: 20s
    tlsHandshakeTimeout: 5s
    responseHeaderTimeout: 8s
    idleConnTimeout: 45s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yamlCfg), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.LookupConfig(&generic)
		require.NoError(t, err)

		assert.Equal(t, httpspec.NewTimeout(1*time.Minute), cfg.Timeout, "Timeout")
		assert.Equal(t, httpspec.NewTimeout(15*time.Second), cfg.TCPDialTimeout, "TCPDialTimeout")
		assert.Equal(t, httpspec.NewTimeout(20*time.Second), cfg.TCPKeepAlive, "TCPKeepAlive")
		assert.Equal(t, httpspec.NewTimeout(5*time.Second), cfg.TLSHandshakeTimeout, "TLSHandshakeTimeout")
		assert.Equal(t, httpspec.NewTimeout(8*time.Second), cfg.ResponseHeaderTimeout, "ResponseHeaderTimeout")
		assert.Equal(t, httpspec.NewTimeout(45*time.Second), cfg.IdleConnTimeout, "IdleConnTimeout")
	})

	t.Run("last config wins when multiple provided", func(t *testing.T) {
		yamlCfg := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 1s
    tcpDialTimeout: 5s
    tcpKeepAlive: 15s
    tlsHandshakeTimeout: 3s
    responseHeaderTimeout: 8s
    idleConnTimeout: 45s
  - type: http.config.ocm.software/v1alpha1
    timeout: 0s
    tcpDialTimeout: 0s
    tcpKeepAlive: 0s
    tlsHandshakeTimeout: 0s
    responseHeaderTimeout: 0s
    idleConnTimeout: 0s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yamlCfg), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.LookupConfig(&generic)
		require.NoError(t, err)

		assert.Equal(t, httpspec.NewTimeout(0), cfg.Timeout, "Timeout")
		assert.Equal(t, httpspec.NewTimeout(0), cfg.TCPDialTimeout, "TCPDialTimeout")
		assert.Equal(t, httpspec.NewTimeout(0), cfg.TCPKeepAlive, "TCPKeepAlive")
		assert.Equal(t, httpspec.NewTimeout(0), cfg.TLSHandshakeTimeout, "TLSHandshakeTimeout")
		assert.Equal(t, httpspec.NewTimeout(0), cfg.ResponseHeaderTimeout, "ResponseHeaderTimeout")
		assert.Equal(t, httpspec.NewTimeout(0), cfg.IdleConnTimeout, "IdleConnTimeout")
	})

	t.Run("first config preserved when second has no timeouts", func(t *testing.T) {
		yamlCfg := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 1s
    tcpDialTimeout: 5s
    tcpKeepAlive: 15s
    tlsHandshakeTimeout: 3s
    responseHeaderTimeout: 8s
    idleConnTimeout: 45s
  - type: http.config.ocm.software/v1alpha1
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yamlCfg), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.LookupConfig(&generic)
		require.NoError(t, err)

		assert.Equal(t, httpspec.NewTimeout(1*time.Second), cfg.Timeout, "Timeout")
		assert.Equal(t, httpspec.NewTimeout(5*time.Second), cfg.TCPDialTimeout, "TCPDialTimeout")
		assert.Equal(t, httpspec.NewTimeout(15*time.Second), cfg.TCPKeepAlive, "TCPKeepAlive")
		assert.Equal(t, httpspec.NewTimeout(3*time.Second), cfg.TLSHandshakeTimeout, "TLSHandshakeTimeout")
		assert.Equal(t, httpspec.NewTimeout(8*time.Second), cfg.ResponseHeaderTimeout, "ResponseHeaderTimeout")
		assert.Equal(t, httpspec.NewTimeout(45*time.Second), cfg.IdleConnTimeout, "IdleConnTimeout")
	})

	t.Run("defaults applied when no configs provided", func(t *testing.T) {
		yamlCfg := `
type: generic.config.ocm.software/v1
configurations: []
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yamlCfg), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.LookupConfig(&generic)
		require.NoError(t, err)

		assert.Equal(t, &httpspec.DefaultTimeout, cfg.Timeout, "Timeout")
		assert.Equal(t, &httpspec.DefaultTCPDialTimeout, cfg.TCPDialTimeout, "TCPDialTimeout")
		assert.Equal(t, &httpspec.DefaultTCPKeepAlive, cfg.TCPKeepAlive, "TCPKeepAlive")
		assert.Equal(t, &httpspec.DefaultTLSHandshakeTimeout, cfg.TLSHandshakeTimeout, "TLSHandshakeTimeout")
		assert.Equal(t, &httpspec.DefaultResponseHeaderTimeout, cfg.ResponseHeaderTimeout, "ResponseHeaderTimeout")
		assert.Equal(t, &httpspec.DefaultIdleConnTimeout, cfg.IdleConnTimeout, "IdleConnTimeout")
	})

	t.Run("defaults applied when config is nil", func(t *testing.T) {
		cfg, err := httpspec.LookupConfig(nil)
		require.NoError(t, err)

		assert.Equal(t, &httpspec.DefaultTimeout, cfg.Timeout, "Timeout")
		assert.Equal(t, &httpspec.DefaultTCPDialTimeout, cfg.TCPDialTimeout, "TCPDialTimeout")
		assert.Equal(t, &httpspec.DefaultTCPKeepAlive, cfg.TCPKeepAlive, "TCPKeepAlive")
		assert.Equal(t, &httpspec.DefaultTLSHandshakeTimeout, cfg.TLSHandshakeTimeout, "TLSHandshakeTimeout")
		assert.Equal(t, &httpspec.DefaultResponseHeaderTimeout, cfg.ResponseHeaderTimeout, "ResponseHeaderTimeout")
		assert.Equal(t, &httpspec.DefaultIdleConnTimeout, cfg.IdleConnTimeout, "IdleConnTimeout")
	})
}

func TestDuration_MarshalRoundTrip(t *testing.T) {
	tests := []struct {
		duration *httpspec.Timeout
		json     string
	}{
		{httpspec.NewTimeout(30 * time.Second), `"30s"`},
		{httpspec.NewTimeout(5 * time.Minute), `"5m0s"`},
		{httpspec.NewTimeout(0), `"0s"`},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.duration.Value()), func(t *testing.T) {
			data, err := tt.duration.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.json, string(data))

			var parsed httpspec.Timeout
			err = parsed.UnmarshalJSON(data)
			require.NoError(t, err)
			assert.Equal(t, *tt.duration, parsed)
		})
	}
}
