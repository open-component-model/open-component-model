package spec

import (
	"fmt"
	"net/url"
	"strings"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConfigType identifies the checksum-over-HTTP configuration inside the
// central generic OCM config. Named for the transport, not for a specific
// input plugin, so a future rename of the wget package leaves it stable.
const ConfigType = "checksum.http.config.ocm.software"

// Scheme is the runtime scheme this config type registers under, mirroring
// http.config.ocm.software and transfer.config.ocm.software.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the wire format of the checksum-over-HTTP configuration. It steers
// both the wget input method and the wget access resource repository through
// a single knob.
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

	// DefaultChecksumPolicy applies to every wget resource whose spec carries
	// no checksumPolicy and whose host does not match [Config.Hosts].
	DefaultChecksumPolicy *ChecksumPolicy `json:"defaultChecksumPolicy,omitempty"`

	// Hosts maps "host" or "host:port" to per-host overrides; port-qualified
	// entries win over bare hostnames.
	Hosts map[string]*HostConfig `json:"hosts,omitempty"`
}

// HostConfig carries per-host overrides. Unset fields fall through to
// [Config.DefaultChecksumPolicy].
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HostConfig struct {
	ChecksumPolicy *ChecksumPolicy `json:"checksumPolicy,omitempty"`
}

// Validate rejects an unknown [Config.Type]. Nested policies are validated by
// their consumer.
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}
	if c.Type.Name != "" && c.Type.Name != ConfigType {
		return fmt.Errorf("invalid config type %q, expected %s", c.Type.Name, ConfigType)
	}
	return nil
}

// LookupConfig extracts, validates, and merges all [ConfigType] entries from
// cfg. Returns nil when cfg is nil or carries no matching entries.
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

// Merge folds configs left-to-right; later entries win. A non-nil
// DefaultChecksumPolicy replaces earlier ones; Hosts maps union with later
// keys overriding.
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

// PolicyForURL returns the effective checksum policy for rawURL: host-scoped
// wins over the default; a malformed URL yields the default. The returned
// policy is read-only.
func (c *Config) PolicyForURL(rawURL string) *ChecksumPolicy {
	if c == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" && len(c.Hosts) > 0 {
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

// hostKeys returns candidate lookup keys for host, most specific first.
// "host:port" wins over the bare hostname (matching http.config.ocm.software).
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

// normalizeHostKey lowercases "host[:port]" (RFC 3986 §3.2.2) and strips one
// trailing dot from a DNS hostname (RFC 3696 §2). IPv6 literals and IPv4
// addresses are left intact.
func normalizeHostKey(host string) string {
	host = strings.ToLower(host)
	if host == "" || strings.HasPrefix(host, "[") {
		return host
	}
	name, port := host, ""
	if i := strings.LastIndexByte(host, ':'); i >= 0 && !strings.ContainsRune(host[:i], ':') {
		name, port = host[:i], host[i:]
	}
	if strings.HasSuffix(name, ".") {
		lastDot := strings.LastIndexByte(name[:len(name)-1], '.')
		last := name[lastDot+1 : len(name)-1]
		if !isAllDigits(last) {
			name = name[:len(name)-1]
		}
	}
	return name + port
}

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
