package spec

import (
	"fmt"
	"net/url"
	"strings"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConfigType is the identifier of the checksum-over-HTTP configuration inside
// the central generic OCM config. It steers how downloaded HTTP resources are
// verified against a source-side checksum (headers, sibling URLs) and is
// deliberately named for the transport rather than for one input plugin, so a
// future rename of the wget package leaves the on-the-wire config identifier
// stable. Its versioned form (ConfigType/vX) is used when carrying an instance
// under `configurations` — see package doc.
const ConfigType = "checksum.http.config.ocm.software"

// Scheme is the runtime scheme this config type registers under, mirroring the
// pattern used by http.config.ocm.software and transfer.config.ocm.software.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the wire format of the checksum-over-HTTP configuration. It steers
// both the wget input method (Wget/v1 in a component-constructor) and the wget
// access resource repository (Wget/v1 access on an existing component version),
// so descriptor authors and operators steer both paths through a single knob.
// The name is transport-scoped, not plugin-scoped: any future HTTP-based input
// or access type reuses the same config.
//
// Example:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: checksum.http.config.ocm.software/v1alpha1
//	    defaultChecksumPolicy:
//	      onMissing: compute
//	      sources:
//	        - type: httpHeader
//	        - type: externalUrl
//	          algorithms: [sha256, sha1]
//	    hosts:
//	      "repo.example.com":
//	        checksumPolicy:
//	          onMissing: fail
//	          sources: [{type: httpHeader}]
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=checksum.http.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=checksum.http.config.ocm.software
	Type runtime.Type `json:"type"`

	// DefaultChecksumPolicy is applied to every wget resource whose spec does
	// not carry its own checksumPolicy and whose host does not match a
	// [Config.Hosts] entry.
	DefaultChecksumPolicy *ChecksumPolicy `json:"defaultChecksumPolicy,omitempty"`

	// Hosts maps hostname (or hostname:port) to per-host overrides. Entries
	// keyed by "host:port" win over bare-hostname entries, matching the
	// convention used by http.config.ocm.software.
	Hosts map[string]*HostConfig `json:"hosts,omitempty"`
}

// HostConfig carries the per-host overrides. Every field is optional: unset
// fields fall through to [Config.DefaultChecksumPolicy] (and then to nil, i.e.
// compute without verification).
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HostConfig struct {
	// ChecksumPolicy overrides [Config.DefaultChecksumPolicy] for every wget
	// URL whose host matches the map key this HostConfig is stored under.
	ChecksumPolicy *ChecksumPolicy `json:"checksumPolicy,omitempty"`
}

// Validate rejects an unknown [Config.Type]. The nested [ChecksumPolicy]
// values are validated by the input package's own decode path when they are
// consumed; here we accept them as-is to keep the config surface additive.
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}
	if c.Type.Name != "" && c.Type.Name != ConfigType {
		return fmt.Errorf("invalid config type %q, expected %s", c.Type.Name, ConfigType)
	}
	return nil
}

// LookupConfig extracts the wget configuration from a central generic config.
// All entries of type [ConfigType] are decoded, validated, and merged via
// [Merge]. Returns nil if cfg is nil or contains no wget entries.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter checksum-http config: %w", err)
	}
	if len(filtered.Configurations) == 0 {
		return nil, nil
	}
	cfgs := make([]*Config, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var c Config
		if err := Scheme.Convert(entry, &c); err != nil {
			return nil, fmt.Errorf("failed to decode checksum-http config: %w", err)
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("invalid checksum-http config: %w", err)
		}
		cfgs = append(cfgs, &c)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config. Later entries win:
// a non-nil DefaultChecksumPolicy overrides earlier ones; hosts maps are
// unioned with later host keys overriding earlier ones. Nil inputs are ignored.
func Merge(configs ...*Config) *Config {
	var out *Config
	for _, c := range configs {
		if c == nil {
			continue
		}
		if out == nil {
			out = &Config{Type: c.Type}
		}
		if c.DefaultChecksumPolicy != nil {
			out.DefaultChecksumPolicy = c.DefaultChecksumPolicy
		}
		for k, v := range c.Hosts {
			if out.Hosts == nil {
				out.Hosts = make(map[string]*HostConfig, len(c.Hosts))
			}
			out.Hosts[k] = v
		}
	}
	return out
}

// PolicyForURL returns the effective checksum policy for rawURL, applying the
// precedence documented on the package: host-scoped wins over the default,
// nil is returned when neither is set. A malformed URL yields the default
// (host matching cannot apply).
//
// Callers should treat the returned policy as read-only.
func (c *Config) PolicyForURL(rawURL string) *ChecksumPolicy {
	if c == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" && len(c.Hosts) > 0 {
		// Normalise config keys once: lowercase (RFC 3986 §3.2.2 makes host
		// case-insensitive but url.Parse preserves the user's case and Go
		// map lookup is exact) and strip one terminal dot from a DNS
		// hostname so a config keyed "repo.example.com" matches a URL that
		// reaches the resolver as "repo.example.com." (RFC 3696 §2). Ports
		// and IP literals are preserved.
		lower := make(map[string]*HostConfig, len(c.Hosts))
		for k, v := range c.Hosts {
			lower[normalizeHostKey(k)] = v
		}
		for _, key := range hostKeys(u.Host) {
			if hc := lower[key]; hc != nil && hc.ChecksumPolicy != nil {
				return hc.ChecksumPolicy
			}
		}
	}
	return c.DefaultChecksumPolicy
}

// hostKeys returns the [Config.Hosts] keys to try for host, most specific first,
// all lowercased for a case-insensitive host match (RFC 3986 §3.2.2).
//
// An entry keyed by the bare hostname applies to every port on that host, and
// one keyed "host:port" applies to that port alone and wins where both are
// present — the same rule http.config.ocm.software uses. A terminal dot on a
// DNS hostname (RFC 3696 §2) is stripped so the URL and config-key views
// agree.
func hostKeys(host string) []string {
	if host == "" {
		return nil
	}
	host = normalizeHostKey(host)
	if name := (&url.URL{Host: host}).Hostname(); name != host {
		return []string{host, name}
	}
	return []string{host}
}

// normalizeHostKey lowercases the "host[:port]" and strips one trailing dot
// from a DNS hostname component. IPv6 literals ("[::1]") and bare IPs are left
// intact.
func normalizeHostKey(host string) string {
	host = strings.ToLower(host)
	if host == "" || strings.HasPrefix(host, "[") {
		return host
	}
	// Split off port, if any, without touching IPv6 literals (already handled).
	name, port := host, ""
	if i := strings.LastIndexByte(host, ':'); i >= 0 && !strings.ContainsRune(host[:i], ':') {
		name, port = host[:i], host[i:]
	}
	// Only strip when it isn't an IPv4 literal ending in a dot ("127.0.0.").
	// Simple heuristic: if the last label is non-numeric, it's a DNS name.
	if strings.HasSuffix(name, ".") {
		lastDot := strings.LastIndexByte(name[:len(name)-1], '.')
		last := name[lastDot+1 : len(name)-1]
		if !isAllDigits(last) {
			name = name[:len(name)-1]
		}
	}
	return name + port
}

// isAllDigits reports whether s consists entirely of ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
