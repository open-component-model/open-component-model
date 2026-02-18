package spec_test

import (
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
			expect httpspec.Timeout
		}{
			{
				name: "parses string like 5m",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 5m
`,
				expect: httpspec.Timeout(5 * time.Minute),
			},
			{
				name: "parses nanoseconds number",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 300000000000
`,
				expect: httpspec.Timeout(5 * time.Minute),
			},
			{
				name: "defaults to zero when omitted",
				yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
`,
				expect: httpspec.Timeout(0),
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
