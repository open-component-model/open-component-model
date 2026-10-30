package runtime_test

import (
	"testing"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIdentityMatchesPath(t *testing.T) {
	type args struct {
		a runtime.Identity
		b runtime.Identity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"match on emptiness",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "",
				},
			},
			true,
		},
		{
			"match on equal paths",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "path",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "path",
				},
			},
			true,
		},
		{
			"no match on diffing paths",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "path",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "different-path",
				},
			},
			false,
		},
		{
			"no match with same base but different subpath",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/different-path",
				},
			},
			false,
		},
		{
			"match based on * pattern",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/*",
				},
			},
			true,
		},
		{
			"no match based on * pattern but different subpath",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path/abc",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/*",
				},
			},
			false,
		},
		{
			"match based on * pattern but different subpath (explicit double *)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path/abc",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/*/*",
				},
			},
			true,
		},
		{
			"match based on ** pattern across segments",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path/abc",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/**",
				},
			},
			true,
		},
		{
			"match based on ** pattern on a single segment",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/**",
				},
			},
			true,
		},
		{
			"no match based on ** pattern on the base itself",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/**",
				},
			},
			false,
		},
		{
			"no match based on ** pattern on a sibling with the same prefix",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base-other/path",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/**",
				},
			},
			false,
		},
		{
			"match based on * pattern within a segment",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/repo.git",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/repo*",
				},
			},
			true,
		},
		{
			"no match on negated class with ^",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/b",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/[^b]",
				},
			},
			false,
		},
		{
			"match on negated class with ^",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/c",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/[^b]",
				},
			},
			true,
		},
		{
			"match on escaped class bracket followed by ^",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/[^b]",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: `base/\[^b\]`,
				},
			},
			true,
		},
		{
			"no match on malformed pattern",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/[",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/[",
				},
			},
			false,
		},
		{
			"match based on * pattern in middle segment",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path/abc",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/*/abc",
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtime.IdentityMatchesPath(tt.args.a, tt.args.b); got != tt.want {
				t.Errorf("IdentityMatchesPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_Match(t *testing.T) {
	type args struct {
		o        runtime.Identity
		matchers []runtime.ChainableIdentityMatcher
	}
	tests := []struct {
		name string
		i    runtime.Identity
		args args
		want bool
	}{
		{
			"empty",
			runtime.Identity{},
			args{
				o:        runtime.Identity{},
				matchers: nil,
			},
			true,
		},
		{
			"equality",
			runtime.Identity{
				"key": "value",
			},
			args{
				o: runtime.Identity{
					"key": "value",
				},
				matchers: nil,
			},
			true,
		},
		{
			"match based on * pattern",
			runtime.Identity{
				runtime.IdentityAttributePath: "base/path",
			},
			args{
				o: runtime.Identity{
					runtime.IdentityAttributePath: "base/*",
				},
			},
			true,
		},
		{
			"match based on * pattern but only with equality matcher",
			runtime.Identity{
				runtime.IdentityAttributePath: "base/path",
			},
			args{
				o: runtime.Identity{
					runtime.IdentityAttributePath: "base/*",
				},
				matchers: []runtime.ChainableIdentityMatcher{
					runtime.IdentityMatchingChainFn(runtime.IdentityEqual),
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.i.Match(tt.args.o, tt.args.matchers...); got != tt.want {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentitySubset(t *testing.T) {
	type args struct {
		base runtime.Identity
		sub  runtime.Identity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"empty subset of empty",
			args{
				sub:  runtime.Identity{},
				base: runtime.Identity{},
			},
			true,
		},
		{
			"empty subset of non-empty",
			args{
				sub: runtime.Identity{},
				base: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
			},
			true,
		},
		{
			"non-empty subset of empty",
			args{
				sub: runtime.Identity{
					"key1": "value1",
				},
				base: runtime.Identity{},
			},
			false,
		},
		{
			"exact match",
			args{
				sub: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
				base: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
			},
			true,
		},
		{
			"proper subset",
			args{
				sub: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
				base: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				},
			},
			true,
		},
		{
			"subset with different value",
			args{
				sub: runtime.Identity{
					"key1": "value1",
					"key2": "different-value",
				},
				base: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
			},
			false,
		},
		{
			"subset with extra key",
			args{
				sub: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				},
				base: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
			},
			false,
		},
		{
			"subset with non-existent key",
			args{
				sub: runtime.Identity{
					"key1": "value1",
					"key3": "value3",
				},
				base: runtime.Identity{
					"key1": "value1",
					"key2": "value2",
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtime.IdentitySubset(tt.args.sub, tt.args.base); got != tt.want {
				t.Errorf("IdentitySubset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentityMatchesURL(t *testing.T) {
	type args struct {
		a runtime.Identity
		b runtime.Identity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"https with explicit port 443 matches https without port",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributePort:     "443",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"http with explicit port 80 matches http without port",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "80",
					runtime.IdentityAttributeScheme:   "http",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "http",
				},
			},
			true,
		},
		{
			"https with non-default port does not match https without port",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributePort:     "8080",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			false,
		},
		{
			"different schemes do not match",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "http",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			false,
		},
		{
			"different hosts do not match",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "other.com",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			false,
		},
		{
			"both without ports match",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"both with same explicit port match",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "8080",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "8080",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"no scheme but one has port - does not match",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "443",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
				},
			},
			false,
		},
		{
			"oci scheme matches https scheme",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "oci",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"empty scheme matches https",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"empty scheme matches http",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "http",
				},
			},
			true,
		},
		{
			"empty scheme with default port matches https",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributePort:     "443",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"empty scheme with default port matches http",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "80",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributeScheme:   "http",
				},
			},
			true,
		},
		{
			"oci scheme with port 443 matches https without port",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "oci",
					runtime.IdentityAttributePort:     "443",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"empty identities match",
			args{
				a: runtime.Identity{},
				b: runtime.Identity{},
			},
			true,
		},
		{
			"https without port matches https with explicit port 443 (reverse)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "ghcr.io",
					runtime.IdentityAttributePort:     "443",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			true,
		},
		{
			"both have different non-default ports",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "8080",
					runtime.IdentityAttributeScheme:   "https",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
					runtime.IdentityAttributePort:     "9090",
					runtime.IdentityAttributeScheme:   "https",
				},
			},
			false,
		},
		{
			"only hostname, no scheme or port - match",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
				},
			},
			true,
		},
		{
			"absent hostname in consumer identity matches absent request hostname",
			args{
				a: runtime.Identity{},
				b: runtime.Identity{},
			},
			true,
		},
		{
			"absent hostname in consumer identity does not match present request hostname",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "registry.example.com",
				},
				b: runtime.Identity{},
			},
			false,
		},
		{
			"consumer hostname present but request hostname absent does not match",
			args{
				a: runtime.Identity{},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			false,
		},
		{
			"explicit empty hostname in consumer identity does not match non-empty hostnames",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "registry.example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "",
				},
			},
			false,
		},
		{
			"explicit empty hostname in consumer identity only matches empty hostname",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "",
				},
			},
			true,
		},
		{
			"hostname matching is case-sensitive (wildcard pattern)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "Sub.Example.COM",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			false,
		},
		{
			"* does not match any hostname (literal)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "localhost",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*",
				},
			},
			false,
		},
		{
			"** does not match any hostname (literal)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "registry.example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "**",
				},
			},
			false,
		},
		{
			"* does not match empty hostname (literal)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*",
				},
			},
			false,
		},
		{
			"** does not match empty hostname (literal)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "**",
				},
			},
			false,
		},
		{
			"*.example.com matches direct subdomain",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "sub.example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			true,
		},
		{
			"*.example.com does not match apex domain",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			false,
		},
		{
			"*.example.com does not match sub-sub domain",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "a.b.example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			false,
		},
		{
			"*.example.com does not match unrelated domain",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "sub.other.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			false,
		},
		{
			"**.example.com matches literally",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "sub.example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "**.example.com",
				},
			},
			false,
		},
		{
			"*.2.3.4 matches 1.2.3.4 (wildcard first label)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "1.2.3.4",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.2.3.4",
				},
			},
			true,
		},
		{
			"*.*.* matches literally",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: "a.b.c",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.*.*",
				},
			},
			false,
		},
		{
			"*.example.com does not match malformed leading-dot hostname",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributeHostname: ".example.com",
				},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "*.example.com",
				},
			},
			false,
		},
		{
			"explicit empty hostname in consumer identity matches absent request hostname",
			args{
				a: runtime.Identity{},
				b: runtime.Identity{
					runtime.IdentityAttributeHostname: "",
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtime.IdentityMatchesURL(tt.args.a, tt.args.b); got != tt.want {
				t.Errorf("IdentityMatchesURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_Match_WithURLMatching(t *testing.T) {
	tests := []struct {
		name string
		i    runtime.Identity
		o    runtime.Identity
		want bool
	}{
		{
			"full URL match with default port normalization",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributeScheme:   "https",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributeScheme:   "https",
			},
			true,
		},
		{
			"URL match with additional attributes",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributeScheme:   "https",
				"repository":                      "myrepo",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributeScheme:   "https",
				"repository":                      "myrepo",
			},
			true,
		},
		{
			"URL match fails due to different additional attributes",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributeScheme:   "https",
				"repository":                      "myrepo",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributeScheme:   "https",
				"repository":                      "otherrepo",
			},
			false,
		},
		{
			"URL with path pattern matching",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePath:     "org/repo",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePath:     "org/*",
			},
			true,
		},
		{
			"single wildcard hostname and path both match",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "sub.example.com",
				runtime.IdentityAttributePath:     "org/repo",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "*.example.com",
				runtime.IdentityAttributePath:     "org/*",
			},
			true,
		},
		{
			"**.example.com matches literally",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "a.b.example.com",
				runtime.IdentityAttributePath:     "org/team/repo",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "**.example.com",
				runtime.IdentityAttributePath:     "org/**",
			},
			false,
		},
		{
			"single wildcard path does not match multi-segment path even if hostname matches",
			runtime.Identity{
				runtime.IdentityAttributeHostname: "sub.example.com",
				runtime.IdentityAttributePath:     "org/team/repo",
			},
			runtime.Identity{
				runtime.IdentityAttributeHostname: "*.example.com",
				runtime.IdentityAttributePath:     "org/*",
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.i.Match(tt.o); got != tt.want {
				t.Errorf("Identity.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}
