package v1alpha1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	cachespec "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestLookupConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		expect    cachespec.RemotePolicy
		expectNil bool
		wantErr   string
	}{
		{
			name: "returns nil without an entry",
			yaml: `
type: generic.config.ocm.software/v1
configurations: []
`,
			expectNil: true,
		},
		{
			name: "leaves the policy empty when the field is omitted",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
`,
			expect: "",
		},
		{
			name: "parses Always",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
`,
			expect: cachespec.RemotePolicyAlways,
		},
		{
			name: "accepts the unversioned type",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software
    remotePolicy: Always
`,
			expect: cachespec.RemotePolicyAlways,
		},
		{
			name: "last entry wins",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: IfNotPresent
`,
			expect: cachespec.RemotePolicyIfNotPresent,
		},
		{
			name: "an entry without the field keeps the earlier value",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
  - type: oci.cache.config.ocm.software/v1alpha1
`,
			expect: cachespec.RemotePolicyAlways,
		},
		{
			name: "rejects an unknown policy",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: always
`,
			wantErr: `unknown remotePolicy "always"`,
		},
		{
			name: "rejects an unknown policy that a later entry overrides",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Never
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
`,
			wantErr: `unknown remotePolicy "Never"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic))

			cfg, err := cachespec.LookupConfig(&generic)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			if tt.expectNil {
				r.Nil(cfg)
				return
			}
			r.NotNil(cfg)
			r.Equal(tt.expect, cfg.RemotePolicy)
		})
	}

	t.Run("nil config returns nil", func(t *testing.T) {
		r := require.New(t)
		cfg, err := cachespec.LookupConfig(nil)
		r.NoError(err)
		r.Nil(cfg)
	})
}

func TestLookupConfig_TTLAndMaxBlobSize(t *testing.T) {
	tests := []struct {
		name            string
		entries         []string
		wantTTL         *time.Duration
		wantMaxBlobSize *int64
		wantErr         string
	}{
		{
			name:    "leaves both unset when omitted",
			entries: []string{"remotePolicy: Always"},
		},
		{
			name:            "parses both",
			entries:         []string{"ttl: 1h30m\nmaxBlobSize: 1024"},
			wantTTL:         new(90 * time.Minute),
			wantMaxBlobSize: new(int64(1024)),
		},
		{
			name:            "last set value wins per field",
			entries:         []string{"ttl: 1m\nmaxBlobSize: 100", "ttl: 2m", "remotePolicy: Always"},
			wantTTL:         new(2 * time.Minute),
			wantMaxBlobSize: new(int64(100)),
		},
		{name: "rejects a malformed ttl", entries: []string{"ttl: soon"}, wantErr: `invalid duration "soon"`},
		{name: "rejects a numeric ttl", entries: []string{"ttl: 1000000000"}, wantErr: "duration must be a string"},
		{name: "rejects a zero ttl", entries: []string{"ttl: 0s"}, wantErr: "invalid ttl 0s, must be positive"},
		{name: "rejects a negative ttl", entries: []string{"ttl: -5m"}, wantErr: "invalid ttl -5m0s, must be positive"},
		{name: "rejects a zero maxBlobSize", entries: []string{"maxBlobSize: 0"}, wantErr: "invalid maxBlobSize 0, must be positive"},
		{name: "rejects a negative maxBlobSize", entries: []string{"maxBlobSize: -1"}, wantErr: "invalid maxBlobSize -1, must be positive"},
		{name: "rejects a non-numeric maxBlobSize", entries: []string{"maxBlobSize: big"}, wantErr: "failed to decode oci cache config"},
		{
			name:    "rejects an invalid value that a later entry overrides",
			entries: []string{"ttl: 0s", "ttl: 1m"},
			wantErr: "invalid ttl 0s, must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			yaml := "type: generic.config.ocm.software/v1\nconfigurations:\n"
			for _, entry := range tt.entries {
				yaml += "  - type: oci.cache.config.ocm.software/v1alpha1\n"
				for _, line := range strings.Split(entry, "\n") {
					yaml += "    " + line + "\n"
				}
			}
			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader(yaml), &generic))

			cfg, err := cachespec.LookupConfig(&generic)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.NotNil(cfg)
			if tt.wantTTL == nil {
				r.Nil(cfg.TTL)
			} else {
				r.NotNil(cfg.TTL)
				r.Equal(*tt.wantTTL, time.Duration(*cfg.TTL))
			}
			r.Equal(tt.wantMaxBlobSize, cfg.MaxBlobSize)
		})
	}
}

func TestLookupConfig_MaxTotalSize(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		want    *int64
		wantErr string
	}{
		{name: "leaves it unset when omitted", entries: []string{"remotePolicy: Always"}},
		{name: "parses it", entries: []string{"maxTotalSize: 1073741824"}, want: new(int64(1 << 30))},
		{name: "accepts zero as no limit", entries: []string{"maxTotalSize: 0"}, want: new(int64(0))},
		{name: "last set value wins", entries: []string{"maxTotalSize: 100", "maxTotalSize: 0", "ttl: 1m"}, want: new(int64(0))},
		{name: "rejects a negative value", entries: []string{"maxTotalSize: -1"}, wantErr: "invalid maxTotalSize -1, must not be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			yaml := "type: generic.config.ocm.software/v1\nconfigurations:\n"
			for _, entry := range tt.entries {
				yaml += "  - type: oci.cache.config.ocm.software/v1alpha1\n    " + entry + "\n"
			}
			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader(yaml), &generic))

			cfg, err := cachespec.LookupConfig(&generic)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, cfg.MaxTotalSize)
		})
	}
}

func TestDuration_JSON(t *testing.T) {
	r := require.New(t)

	data, err := json.Marshal(cachespec.NewDuration(90 * time.Minute))
	r.NoError(err)
	r.JSONEq(`"1h30m0s"`, string(data))

	var decoded cachespec.Duration
	r.NoError(json.Unmarshal(data, &decoded))
	r.Equal(90*time.Minute, time.Duration(decoded))
}
