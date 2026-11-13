package v1alpha1_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	typesv1 "ocm.software/open-component-model/bindings/go/configuration/types/v1"
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

func TestLookupConfig_Sections(t *testing.T) {
	dur := typesv1.NewDuration
	tests := []struct {
		name          string
		entries       []string
		wantBlob      *cachespec.BlobCacheConfig
		wantReference *cachespec.ReferenceCacheConfig
		wantErr       string
	}{
		{name: "leaves both sections unset when omitted", entries: []string{"remotePolicy: Always"}},
		{
			name:    "parses every field",
			entries: []string{"blob:\n  enabled: false\n  ttl: 24h\n  maxBlobSize: 1024\n  maxTotalSize: 4096\nreference:\n  enabled: true\n  ttl: 1m\n  maxEntries: 500"},
			wantBlob: &cachespec.BlobCacheConfig{
				Enabled: new(false), TTL: dur(24 * time.Hour), MaxBlobSize: new(int64(1024)), MaxTotalSize: new(int64(4096)),
			},
			wantReference: &cachespec.ReferenceCacheConfig{Enabled: new(true), TTL: dur(time.Minute), MaxEntries: new(500)},
		},
		{
			name:          "last set value wins per field",
			entries:       []string{"blob:\n  ttl: 1m\n  maxBlobSize: 100\nreference:\n  ttl: 1m\n  maxEntries: 5", "blob:\n  ttl: 2m", "reference:\n  enabled: false\n  maxEntries: 7"},
			wantBlob:      &cachespec.BlobCacheConfig{TTL: dur(2 * time.Minute), MaxBlobSize: new(int64(100))},
			wantReference: &cachespec.ReferenceCacheConfig{Enabled: new(false), TTL: dur(time.Minute), MaxEntries: new(7)},
		},
		{
			name:     "accepts zero maxTotalSize as no limit",
			entries:  []string{"blob:\n  maxTotalSize: 0"},
			wantBlob: &cachespec.BlobCacheConfig{MaxTotalSize: new(int64(0))},
		},
		{
			name:          "parses a number as nanoseconds",
			entries:       []string{"reference:\n  ttl: 1000000000"},
			wantReference: &cachespec.ReferenceCacheConfig{TTL: dur(time.Second)},
		},
		{name: "rejects a malformed ttl", entries: []string{"blob:\n  ttl: soon"}, wantErr: `invalid duration "soon"`},
		{name: "rejects a zero blob ttl", entries: []string{"blob:\n  ttl: 0s"}, wantErr: "invalid blob.ttl 0s, must be positive"},
		{name: "rejects a negative reference ttl", entries: []string{"reference:\n  ttl: -5m"}, wantErr: "invalid reference.ttl -5m0s, must be positive"},
		{name: "rejects a zero maxBlobSize", entries: []string{"blob:\n  maxBlobSize: 0"}, wantErr: "invalid blob.maxBlobSize 0, must be positive"},
		{name: "rejects a zero maxEntries", entries: []string{"reference:\n  maxEntries: 0"}, wantErr: "invalid reference.maxEntries 0, must be positive"},
		{name: "rejects a negative maxTotalSize", entries: []string{"blob:\n  maxTotalSize: -1"}, wantErr: "invalid blob.maxTotalSize -1, must not be negative"},
		{name: "rejects a non-numeric maxBlobSize", entries: []string{"blob:\n  maxBlobSize: big"}, wantErr: "failed to decode oci cache config"},
		{
			name:    "rejects an invalid value that a later entry overrides",
			entries: []string{"blob:\n  ttl: 0s", "blob:\n  ttl: 1m"},
			wantErr: "invalid blob.ttl 0s, must be positive",
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
			r.Equal(tt.wantBlob, cfg.Blob)
			r.Equal(tt.wantReference, cfg.Reference)
		})
	}
}

func TestConfig_Enabled(t *testing.T) {
	r := require.New(t)
	var unset *cachespec.Config
	r.True(unset.BlobEnabled(true))
	r.False(unset.ReferenceEnabled(false))

	cfg := &cachespec.Config{
		Blob:      &cachespec.BlobCacheConfig{Enabled: new(false)},
		Reference: &cachespec.ReferenceCacheConfig{Enabled: new(true)},
	}
	r.False(cfg.BlobEnabled(true))
	r.True(cfg.ReferenceEnabled(false))

	empty := &cachespec.Config{Blob: &cachespec.BlobCacheConfig{}, Reference: &cachespec.ReferenceCacheConfig{}}
	r.True(empty.BlobEnabled(true), "an unset enabled keeps the default")
	r.False(empty.ReferenceEnabled(false), "an unset enabled keeps the default")
}
