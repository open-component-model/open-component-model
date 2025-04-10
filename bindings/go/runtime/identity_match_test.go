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
			"no match based on * pattern but different subpath (explicit double * with no path)",
			args{
				a: runtime.Identity{
					runtime.IdentityAttributePath: "base/path/abc",
				},
				b: runtime.Identity{
					runtime.IdentityAttributePath: "base/**",
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
