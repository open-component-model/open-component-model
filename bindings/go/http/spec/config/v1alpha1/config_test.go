package v1alpha1_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpspec "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
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
				name: "nil when omitted",
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

	t.Run("transport-level timeouts", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 2m
    tcpDialTimeout: 15s
    tcpKeepAlive: 30s
    tlsHandshakeTimeout: 5s
    responseHeaderTimeout: 10s
    idleConnTimeout: 60s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)
		require.Len(t, generic.Configurations, 1)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.Timeout)
		assert.Equal(t, httpspec.Timeout(2*time.Minute), *cfg.Timeout)
		require.NotNil(t, cfg.TCPDialTimeout)
		assert.Equal(t, httpspec.Timeout(15*time.Second), *cfg.TCPDialTimeout)
		require.NotNil(t, cfg.TCPKeepAlive)
		assert.Equal(t, httpspec.Timeout(30*time.Second), *cfg.TCPKeepAlive)
		require.NotNil(t, cfg.TLSHandshakeTimeout)
		assert.Equal(t, httpspec.Timeout(5*time.Second), *cfg.TLSHandshakeTimeout)
		require.NotNil(t, cfg.ResponseHeaderTimeout)
		assert.Equal(t, httpspec.Timeout(10*time.Second), *cfg.ResponseHeaderTimeout)
		require.NotNil(t, cfg.IdleConnTimeout)
		assert.Equal(t, httpspec.Timeout(60*time.Second), *cfg.IdleConnTimeout)
	})

	t.Run("transport-level timeouts nil when omitted", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 30s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.Timeout)
		assert.Equal(t, httpspec.Timeout(30*time.Second), *cfg.Timeout)
		assert.Nil(t, cfg.TCPDialTimeout)
		assert.Nil(t, cfg.TCPKeepAlive)
		assert.Nil(t, cfg.TLSHandshakeTimeout)
		assert.Nil(t, cfg.ResponseHeaderTimeout)
		assert.Nil(t, cfg.IdleConnTimeout)
	})

	t.Run("per-host overrides", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 30s
    hosts:
      "ghcr.io:443":
        timeout: 60s
        tlsHandshakeTimeout: 5s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.Timeout)
		assert.Equal(t, httpspec.Timeout(30*time.Second), *cfg.Timeout)
		require.Contains(t, cfg.Hosts, "ghcr.io:443")

		host := cfg.Hosts["ghcr.io:443"]
		require.NotNil(t, host.Timeout)
		assert.Equal(t, httpspec.Timeout(60*time.Second), *host.Timeout)
		require.NotNil(t, host.TLSHandshakeTimeout)
		assert.Equal(t, httpspec.Timeout(5*time.Second), *host.TLSHandshakeTimeout)
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

func TestResolveHTTPConfig(t *testing.T) {
	t.Run("nil input returns Config with default timeout", func(t *testing.T) {
		cfg, err := httpspec.ResolveHTTPConfig(nil)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		require.NotNil(t, cfg.Timeout)
		assert.Equal(t, httpspec.Timeout(time.Duration(httpspec.DefaultTimeout)), *cfg.Timeout)
	})

	t.Run("valid config returns resolved Config", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 1m
    tcpDialTimeout: 5s
    hosts:
      "ghcr.io:443":
        timeout: 2m
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.ResolveHTTPConfig(&generic)
		require.NoError(t, err)
		require.NotNil(t, cfg.Timeout)
		assert.Equal(t, httpspec.Timeout(1*time.Minute), *cfg.Timeout)
		require.NotNil(t, cfg.TCPDialTimeout)
		assert.Equal(t, httpspec.Timeout(5*time.Second), *cfg.TCPDialTimeout)
		require.Contains(t, cfg.Hosts, "ghcr.io:443")
	})

	t.Run("invalid config (negative timeout) returns error", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: -1s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.ResolveHTTPConfig(&generic)
		assert.Nil(t, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid http configuration")
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("invalid per-host timeout returns error wrapped with host key", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    hosts:
      "ghcr.io:443":
        timeout: -5s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		cfg, err := httpspec.ResolveHTTPConfig(&generic)
		assert.Nil(t, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `host "ghcr.io:443"`)
	})
}

func TestMerge(t *testing.T) {
	t.Run("last non-nil pointer wins for timeouts", func(t *testing.T) {
		a := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout:        httpspec.NewTimeout(1 * time.Minute),
				TCPDialTimeout: httpspec.NewTimeout(10 * time.Second),
				TCPKeepAlive:   httpspec.NewTimeout(15 * time.Second),
			},
		}
		b := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout:             httpspec.NewTimeout(2 * time.Minute),
				TCPDialTimeout:      httpspec.NewTimeout(20 * time.Second),
				TLSHandshakeTimeout: httpspec.NewTimeout(5 * time.Second),
			},
		}

		merged := httpspec.Merge(a, b)

		require.NotNil(t, merged.Timeout)
		assert.Equal(t, httpspec.Timeout(2*time.Minute), *merged.Timeout)
		require.NotNil(t, merged.TCPDialTimeout)
		assert.Equal(t, httpspec.Timeout(20*time.Second), *merged.TCPDialTimeout)
		require.NotNil(t, merged.TCPKeepAlive)
		assert.Equal(t, httpspec.Timeout(15*time.Second), *merged.TCPKeepAlive)
		require.NotNil(t, merged.TLSHandshakeTimeout)
		assert.Equal(t, httpspec.Timeout(5*time.Second), *merged.TLSHandshakeTimeout)
		assert.Nil(t, merged.ResponseHeaderTimeout)
		assert.Nil(t, merged.IdleConnTimeout)
	})

	t.Run("nil fields do not override", func(t *testing.T) {
		a := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				IdleConnTimeout: httpspec.NewTimeout(90 * time.Second),
			},
		}
		b := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout: httpspec.NewTimeout(30 * time.Second),
			},
		}

		merged := httpspec.Merge(a, b)

		require.NotNil(t, merged.Timeout)
		assert.Equal(t, httpspec.Timeout(30*time.Second), *merged.Timeout)
		require.NotNil(t, merged.IdleConnTimeout)
		assert.Equal(t, httpspec.Timeout(90*time.Second), *merged.IdleConnTimeout)
	})

	t.Run("hosts merge maps", func(t *testing.T) {
		a := &httpspec.Config{
			Hosts: map[string]*httpspec.HostConfig{
				"a.com": {TimeoutConfig: httpspec.TimeoutConfig{Timeout: httpspec.NewTimeout(10 * time.Second)}},
			},
		}
		b := &httpspec.Config{
			Hosts: map[string]*httpspec.HostConfig{
				"b.com": {TimeoutConfig: httpspec.TimeoutConfig{IdleConnTimeout: httpspec.NewTimeout(60 * time.Second)}},
			},
		}

		merged := httpspec.Merge(a, b)

		require.Len(t, merged.Hosts, 2)
		require.NotNil(t, merged.Hosts["a.com"].Timeout)
		assert.Equal(t, httpspec.Timeout(10*time.Second), *merged.Hosts["a.com"].Timeout)
		require.NotNil(t, merged.Hosts["b.com"].IdleConnTimeout)
		assert.Equal(t, httpspec.Timeout(60*time.Second), *merged.Hosts["b.com"].IdleConnTimeout)
	})

	t.Run("nil element is skipped", func(t *testing.T) {
		a := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout: httpspec.NewTimeout(10 * time.Second),
			},
		}
		b := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				IdleConnTimeout: httpspec.NewTimeout(60 * time.Second),
			},
		}
		merged := httpspec.Merge(a, nil, b)
		require.NotNil(t, merged.Timeout)
		assert.Equal(t, httpspec.Timeout(10*time.Second), *merged.Timeout)
		require.NotNil(t, merged.IdleConnTimeout)
		assert.Equal(t, httpspec.Timeout(60*time.Second), *merged.IdleConnTimeout)
	})

	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, httpspec.Merge())
	})

	t.Run("last non-nil wins across three configs", func(t *testing.T) {
		a := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout:        httpspec.NewTimeout(1 * time.Minute),
				TCPDialTimeout: httpspec.NewTimeout(10 * time.Second),
				TCPKeepAlive:   httpspec.NewTimeout(15 * time.Second),
			},
		}
		b := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				TCPDialTimeout:      httpspec.NewTimeout(20 * time.Second),
				TLSHandshakeTimeout: httpspec.NewTimeout(5 * time.Second),
			},
		}
		c := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout:         httpspec.NewTimeout(3 * time.Minute),
				IdleConnTimeout: httpspec.NewTimeout(90 * time.Second),
			},
		}

		merged := httpspec.Merge(a, b, c)

		require.NotNil(t, merged.Timeout)
		assert.Equal(t, httpspec.Timeout(3*time.Minute), *merged.Timeout, "c overrides a")
		require.NotNil(t, merged.TCPDialTimeout)
		assert.Equal(t, httpspec.Timeout(20*time.Second), *merged.TCPDialTimeout, "b overrides a, c does not touch")
		require.NotNil(t, merged.TCPKeepAlive)
		assert.Equal(t, httpspec.Timeout(15*time.Second), *merged.TCPKeepAlive, "carried from a, neither b nor c set it")
		require.NotNil(t, merged.TLSHandshakeTimeout)
		assert.Equal(t, httpspec.Timeout(5*time.Second), *merged.TLSHandshakeTimeout, "set only by b")
		require.NotNil(t, merged.IdleConnTimeout)
		assert.Equal(t, httpspec.Timeout(90*time.Second), *merged.IdleConnTimeout, "set only by c")
		assert.Nil(t, merged.ResponseHeaderTimeout, "untouched by any input")
	})
}

func TestTimeoutConfig_Validate(t *testing.T) {
	t.Run("nil and zero pointers are valid", func(t *testing.T) {
		assert.NoError(t, (&httpspec.TimeoutConfig{}).Validate())
		assert.NoError(t, (&httpspec.TimeoutConfig{
			Timeout:               httpspec.NewTimeout(0),
			TCPDialTimeout:        httpspec.NewTimeout(0),
			TLSHandshakeTimeout:   httpspec.NewTimeout(0),
			ResponseHeaderTimeout: httpspec.NewTimeout(0),
			IdleConnTimeout:       httpspec.NewTimeout(0),
		}).Validate())
	})

	t.Run("negative TCPKeepAlive is allowed (means disabled)", func(t *testing.T) {
		assert.NoError(t, (&httpspec.TimeoutConfig{
			TCPKeepAlive: httpspec.NewTimeout(-1 * time.Second),
		}).Validate())
	})

	negativeCases := []struct {
		name  string
		field string
		cfg   httpspec.TimeoutConfig
	}{
		{"timeout", "timeout", httpspec.TimeoutConfig{Timeout: httpspec.NewTimeout(-5 * time.Minute)}},
		{"tcpDialTimeout", "tcpDialTimeout", httpspec.TimeoutConfig{TCPDialTimeout: httpspec.NewTimeout(-10 * time.Second)}},
		{"tlsHandshakeTimeout", "tlsHandshakeTimeout", httpspec.TimeoutConfig{TLSHandshakeTimeout: httpspec.NewTimeout(-1 * time.Hour)}},
		{"responseHeaderTimeout", "responseHeaderTimeout", httpspec.TimeoutConfig{ResponseHeaderTimeout: httpspec.NewTimeout(-1 * time.Second)}},
		{"idleConnTimeout", "idleConnTimeout", httpspec.TimeoutConfig{IdleConnTimeout: httpspec.NewTimeout(-30 * time.Second)}},
	}
	for _, tc := range negativeCases {
		t.Run("rejects negative "+tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.field)
			assert.Contains(t, err.Error(), "must be zero or positive")
		})
	}
}

func TestTimeout_MarshalJSON(t *testing.T) {
	t.Run("marshals to duration string", func(t *testing.T) {
		d := httpspec.Timeout(30 * time.Second)
		b, err := d.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"30s"`, string(b))
	})

	t.Run("zero marshals to 0s", func(t *testing.T) {
		d := httpspec.Timeout(0)
		b, err := d.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"0s"`, string(b))
	})

	t.Run("round-trips through JSON", func(t *testing.T) {
		original := httpspec.Timeout(5 * time.Minute)
		b, err := original.MarshalJSON()
		require.NoError(t, err)
		var got httpspec.Timeout
		require.NoError(t, got.UnmarshalJSON(b))
		assert.Equal(t, original, got)
	})
}

func TestMergeTimeoutConfig_NilSrc(t *testing.T) {
	dst := httpspec.TimeoutConfig{
		Timeout:        httpspec.NewTimeout(30 * time.Second),
		TCPDialTimeout: httpspec.NewTimeout(10 * time.Second),
	}
	result := httpspec.MergeTimeoutConfig(&dst, nil)
	require.NotNil(t, result.Timeout)
	assert.Equal(t, httpspec.Timeout(30*time.Second), *result.Timeout)
	require.NotNil(t, result.TCPDialTimeout)
	assert.Equal(t, httpspec.Timeout(10*time.Second), *result.TCPDialTimeout)
}

func TestMerge_SameHostKeyLastWins(t *testing.T) {
	a := &httpspec.Config{
		Hosts: map[string]*httpspec.HostConfig{
			"ghcr.io": {TimeoutConfig: httpspec.TimeoutConfig{Timeout: httpspec.NewTimeout(10 * time.Second)}},
		},
	}
	b := &httpspec.Config{
		Hosts: map[string]*httpspec.HostConfig{
			"ghcr.io": {TimeoutConfig: httpspec.TimeoutConfig{Timeout: httpspec.NewTimeout(60 * time.Second)}},
		},
	}
	merged := httpspec.Merge(a, b)
	require.Len(t, merged.Hosts, 1)
	require.NotNil(t, merged.Hosts["ghcr.io"].Timeout)
	assert.Equal(t, httpspec.Timeout(60*time.Second), *merged.Hosts["ghcr.io"].Timeout, "b's value must win")
}

func TestConfig_Validate(t *testing.T) {
	t.Run("valid global with valid hosts", func(t *testing.T) {
		cfg := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout:         httpspec.NewTimeout(30 * time.Second),
				IdleConnTimeout: httpspec.NewTimeout(90 * time.Second),
			},
			Hosts: map[string]*httpspec.HostConfig{
				"ghcr.io:443": {TimeoutConfig: httpspec.TimeoutConfig{Timeout: httpspec.NewTimeout(60 * time.Second)}},
			},
		}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("propagates global error", func(t *testing.T) {
		cfg := &httpspec.Config{
			TimeoutConfig: httpspec.TimeoutConfig{
				Timeout: httpspec.NewTimeout(-1 * time.Second),
			},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("wraps host error with host key", func(t *testing.T) {
		cfg := &httpspec.Config{
			Hosts: map[string]*httpspec.HostConfig{
				"ghcr.io:443": {TimeoutConfig: httpspec.TimeoutConfig{Timeout: httpspec.NewTimeout(-5 * time.Second)}},
			},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `host "ghcr.io:443"`)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("nil host entry is skipped", func(t *testing.T) {
		cfg := &httpspec.Config{
			Hosts: map[string]*httpspec.HostConfig{
				"ghcr.io:443": nil,
			},
		}
		assert.NoError(t, cfg.Validate())
	})
}

func TestRetryConfig_ParseYAML(t *testing.T) {
	t.Run("all retry fields parsed", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    retry:
      maxRetries: 3
      minWait: 100ms
      maxWait: 5s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.Retry)
		require.NotNil(t, cfg.Retry.MaxRetries)
		assert.Equal(t, 3, *cfg.Retry.MaxRetries)
		require.NotNil(t, cfg.Retry.MinWait)
		assert.Equal(t, httpspec.Timeout(100*time.Millisecond), *cfg.Retry.MinWait)
		require.NotNil(t, cfg.Retry.MaxWait)
		assert.Equal(t, httpspec.Timeout(5*time.Second), *cfg.Retry.MaxWait)
	})

	t.Run("retry omitted leaves Retry nil", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 30s
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)
		assert.Nil(t, cfg.Retry)
	})

	t.Run("maxRetries zero parses correctly", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    retry:
      maxRetries: 0
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.Retry)
		require.NotNil(t, cfg.Retry.MaxRetries)
		assert.Equal(t, 0, *cfg.Retry.MaxRetries)
	})

	t.Run("per-host retry parses correctly", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    retry:
      maxRetries: 5
    hosts:
      "ghcr.io:443":
        retry:
          maxRetries: 2
          minWait: 50ms
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.Retry)
		assert.Equal(t, 5, *cfg.Retry.MaxRetries)
		require.Contains(t, cfg.Hosts, "ghcr.io:443")
		host := cfg.Hosts["ghcr.io:443"]
		require.NotNil(t, host.Retry)
		assert.Equal(t, 2, *host.Retry.MaxRetries)
		assert.Equal(t, httpspec.Timeout(50*time.Millisecond), *host.Retry.MinWait)
	})
}

func TestRetryConfig_Validate(t *testing.T) {
	t.Run("nil, zero, and -1 are valid", func(t *testing.T) {
		zero := 0
		negOne := -1
		assert.NoError(t, (&httpspec.RetryConfig{}).Validate())
		assert.NoError(t, (&httpspec.RetryConfig{MaxRetries: &zero}).Validate())
		assert.NoError(t, (&httpspec.RetryConfig{MaxRetries: &negOne}).Validate())
	})

	t.Run("rejects MaxRetries below -1", func(t *testing.T) {
		neg := -2
		err := (&httpspec.RetryConfig{MaxRetries: &neg}).Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maxRetries")
		assert.Contains(t, err.Error(), "-1 (disable)")
	})

	t.Run("rejects negative MinWait", func(t *testing.T) {
		err := (&httpspec.RetryConfig{MinWait: httpspec.NewTimeout(-1 * time.Millisecond)}).Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "minWait")
	})

	t.Run("rejects negative MaxWait", func(t *testing.T) {
		err := (&httpspec.RetryConfig{MaxWait: httpspec.NewTimeout(-1 * time.Second)}).Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maxWait")
	})

	t.Run("rejects MinWait greater than MaxWait", func(t *testing.T) {
		err := (&httpspec.RetryConfig{
			MinWait: httpspec.NewTimeout(5 * time.Second),
			MaxWait: httpspec.NewTimeout(1 * time.Second),
		}).Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "minWait")
		assert.Contains(t, err.Error(), "maxWait")
	})

	t.Run("equal MinWait and MaxWait is valid", func(t *testing.T) {
		assert.NoError(t, (&httpspec.RetryConfig{
			MinWait: httpspec.NewTimeout(1 * time.Second),
			MaxWait: httpspec.NewTimeout(1 * time.Second),
		}).Validate())
	})

	t.Run("Config.Validate wraps retry error", func(t *testing.T) {
		neg := -2
		cfg := &httpspec.Config{
			Retry: &httpspec.RetryConfig{MaxRetries: &neg},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid retry config:")
	})

	t.Run("Config.Validate wraps per-host retry error", func(t *testing.T) {
		neg := -2
		cfg := &httpspec.Config{
			Hosts: map[string]*httpspec.HostConfig{
				"ghcr.io:443": {
					Retry: &httpspec.RetryConfig{MaxRetries: &neg},
				},
			},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `host "ghcr.io:443"`)
		assert.Contains(t, err.Error(), "invalid retry config:")
	})
}

func TestRetryConfig_Merge(t *testing.T) {
	t.Run("both nil returns nil", func(t *testing.T) {
		assert.Nil(t, httpspec.MergeRetryConfig(nil, nil))
	})

	t.Run("dst nil src non-nil returns src", func(t *testing.T) {
		two := 2
		src := &httpspec.RetryConfig{MaxRetries: &two}
		result := httpspec.MergeRetryConfig(nil, src)
		require.NotNil(t, result)
		assert.Equal(t, 2, *result.MaxRetries)
	})

	t.Run("src nil dst non-nil returns dst copy", func(t *testing.T) {
		two := 2
		dst := &httpspec.RetryConfig{MaxRetries: &two}
		result := httpspec.MergeRetryConfig(dst, nil)
		require.NotNil(t, result)
		assert.Equal(t, 2, *result.MaxRetries)
	})

	t.Run("last non-nil wins per field", func(t *testing.T) {
		five := 5
		two := 2
		dst := &httpspec.RetryConfig{
			MaxRetries: &five,
			MinWait:    httpspec.NewTimeout(200 * time.Millisecond),
		}
		src := &httpspec.RetryConfig{
			MaxRetries: &two,
			MaxWait:    httpspec.NewTimeout(5 * time.Second),
		}
		result := httpspec.MergeRetryConfig(dst, src)
		require.NotNil(t, result)
		assert.Equal(t, 2, *result.MaxRetries, "src overrides dst")
		assert.Equal(t, httpspec.Timeout(200*time.Millisecond), *result.MinWait, "dst preserved when src nil")
		assert.Equal(t, httpspec.Timeout(5*time.Second), *result.MaxWait, "src set")
	})

	t.Run("Merge wires RetryConfig", func(t *testing.T) {
		five := 5
		two := 2
		a := &httpspec.Config{Retry: &httpspec.RetryConfig{MaxRetries: &five}}
		b := &httpspec.Config{Retry: &httpspec.RetryConfig{MaxRetries: &two}}
		merged := httpspec.Merge(a, b)
		require.NotNil(t, merged.Retry)
		assert.Equal(t, 2, *merged.Retry.MaxRetries, "b overrides a")
	})

	t.Run("Merge preserves Retry when second has nil Retry", func(t *testing.T) {
		five := 5
		a := &httpspec.Config{Retry: &httpspec.RetryConfig{MaxRetries: &five}}
		b := &httpspec.Config{}
		merged := httpspec.Merge(a, b)
		require.NotNil(t, merged.Retry)
		assert.Equal(t, 5, *merged.Retry.MaxRetries)
	})
}

func TestMergeTLSConfig(t *testing.T) {
	tr := true
	fa := false

	t.Run("nil dst and nil src returns zero TLSConfig", func(t *testing.T) {
		result := httpspec.MergeTLSConfig(nil, nil)
		assert.Nil(t, result.InsecureSkipVerify)
	})

	t.Run("dst only preserved when src is nil", func(t *testing.T) {
		dst := &httpspec.TLSConfig{InsecureSkipVerify: &tr}
		result := httpspec.MergeTLSConfig(dst, nil)
		require.NotNil(t, result.InsecureSkipVerify)
		assert.True(t, *result.InsecureSkipVerify)
	})

	t.Run("src overrides dst", func(t *testing.T) {
		dst := &httpspec.TLSConfig{InsecureSkipVerify: &fa}
		src := &httpspec.TLSConfig{InsecureSkipVerify: &tr}
		result := httpspec.MergeTLSConfig(dst, src)
		require.NotNil(t, result.InsecureSkipVerify)
		assert.True(t, *result.InsecureSkipVerify)
	})

	t.Run("false overrides true — per-host can re-enable verification", func(t *testing.T) {
		dst := &httpspec.TLSConfig{InsecureSkipVerify: &tr}
		src := &httpspec.TLSConfig{InsecureSkipVerify: &fa}
		result := httpspec.MergeTLSConfig(dst, src)
		require.NotNil(t, result.InsecureSkipVerify)
		assert.False(t, *result.InsecureSkipVerify)
	})
}

func TestConfig_ParseYAML_TLS(t *testing.T) {
	t.Run("insecureSkipVerify true at global scope", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    insecureSkipVerify: true
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)
		require.Len(t, generic.Configurations, 1)

		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.InsecureSkipVerify)
		assert.True(t, *cfg.InsecureSkipVerify)
	})

	t.Run("insecureSkipVerify per-host", func(t *testing.T) {
		yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    hosts:
      registry.example.com:
        insecureSkipVerify: true
`
		var generic genericv1.Config
		err := genericv1.Scheme.Decode(strings.NewReader(yaml), &generic)
		require.NoError(t, err)
		var cfg httpspec.Config
		err = httpspec.Scheme.Convert(generic.Configurations[0], &cfg)
		require.NoError(t, err)
		assert.Nil(t, cfg.InsecureSkipVerify)
		require.Contains(t, cfg.Hosts, "registry.example.com")
		require.NotNil(t, cfg.Hosts["registry.example.com"].InsecureSkipVerify)
		assert.True(t, *cfg.Hosts["registry.example.com"].InsecureSkipVerify)
	})
}

func TestMerge_TLS(t *testing.T) {
	tr := true
	fa := false

	t.Run("global InsecureSkipVerify propagated through Merge", func(t *testing.T) {
		a := &httpspec.Config{TLSConfig: httpspec.TLSConfig{InsecureSkipVerify: &tr}}
		b := &httpspec.Config{}
		merged := httpspec.Merge(a, b)
		require.NotNil(t, merged.InsecureSkipVerify)
		assert.True(t, *merged.InsecureSkipVerify)
	})

	t.Run("later layer overrides earlier", func(t *testing.T) {
		a := &httpspec.Config{TLSConfig: httpspec.TLSConfig{InsecureSkipVerify: &tr}}
		b := &httpspec.Config{TLSConfig: httpspec.TLSConfig{InsecureSkipVerify: &fa}}
		merged := httpspec.Merge(a, b)
		require.NotNil(t, merged.InsecureSkipVerify)
		assert.False(t, *merged.InsecureSkipVerify)
	})
}
