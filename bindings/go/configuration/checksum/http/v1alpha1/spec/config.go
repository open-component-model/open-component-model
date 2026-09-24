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
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: checksum.http.config.ocm.software/v1alpha1
//	    mode: Prefer
//	    hosts:
//	      "repo.example.com":
//	        mode: Require
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=checksum.http.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=checksum.http.config.ocm.software
	Type runtime.Type `json:"type"`

	// Mode is the default verification behaviour for every wget resource whose
	// host does not match [Config.Hosts]. Defaults to "Prefer" when unset.
	// +ocm:jsonschema-gen:enum=Require,Prefer,Skip
	Mode ChecksumMode `json:"mode,omitempty"`

	// Hosts maps "host" or "host:port" to per-host overrides; port-qualified
	// entries win over bare hostnames.
	Hosts map[string]*ChecksumPolicy `json:"hosts,omitempty"`
}

// Validate rejects an unknown [Config.Type] and any explicitly supplied but
// unrecognised [ChecksumMode] on the top level or a host override. An empty
// mode is allowed and resolves to the default via [ChecksumMode.Normalize].
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}
	if c.Type.Name != "" && c.Type.Name != ConfigType {
		return fmt.Errorf("invalid config type %q, expected %s", c.Type.Name, ConfigType)
	}
	if !c.Mode.Valid() {
		return fmt.Errorf("invalid mode %q, expected one of %s, %s, %s (or empty)", c.Mode, ChecksumModeRequire, ChecksumModePrefer, ChecksumModeSkip)
	}
	for host, policy := range c.Hosts {
		if policy != nil && !policy.Mode.Valid() {
			return fmt.Errorf("invalid mode %q for host %q, expected one of %s, %s, %s (or empty)", policy.Mode, host, ChecksumModeRequire, ChecksumModePrefer, ChecksumModeSkip)
		}
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

// Merge folds configs left-to-right; later entries win. A non-empty Mode
// replaces earlier ones; Hosts maps union with later keys overriding.
func Merge(configs ...*Config) *Config {
	var out *Config
	for _, c := range configs {
		if c == nil {
			continue
		}
		if out == nil {
			out = &Config{Type: c.Type}
		}
		if c.Mode != "" {
			out.Mode = c.Mode
		}
		for k, v := range c.Hosts {
			if out.Hosts == nil {
				out.Hosts = make(map[string]*ChecksumPolicy, len(c.Hosts))
			}
			out.Hosts[k] = v
		}
	}
	return out
}

// ModeForURL returns the effective [ChecksumMode] for rawURL: a host-scoped
// override wins over [Config.Mode]; a malformed URL yields the default. The
// zero value is resolved to [ChecksumModePrefer].
func (c *Config) ModeForURL(rawURL string) ChecksumMode {
	if c == nil {
		return ChecksumModePrefer
	}
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" && len(c.Hosts) > 0 {
		lower := make(map[string]*ChecksumPolicy, len(c.Hosts))
		for k, v := range c.Hosts {
			lower[normalizeHostKey(k)] = v
		}
		for _, key := range hostKeys(u.Host) {
			if hc := lower[key]; hc != nil && hc.Mode != "" {
				return hc.Mode
			}
		}
	}
	return c.Mode.Normalize()
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
