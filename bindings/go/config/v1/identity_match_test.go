package v1_test

import (
	"testing"

	. "ocm.software/open-component-model/bindings/go/config/v1"
)

func TestIdentityEqual(t *testing.T) {
	type args struct {
		i Identity
		o Identity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"empty",
			args{
				i: Identity{},
				o: Identity{},
			},
			true,
		},
		{
			"equal",
			args{
				i: Identity{
					"key": "value",
				},
				o: Identity{
					"key": "value",
				},
			},
			true,
		},
		{
			"not equal",
			args{
				i: Identity{
					"key": "value",
				},
				o: Identity{
					"key": "value2",
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IdentityEqual(tt.args.i, tt.args.o); got != tt.want {
				t.Errorf("IdentityEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentityMatchesPath(t *testing.T) {
	type args struct {
		a Identity
		b Identity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"match on emptiness",
			args{
				a: Identity{
					IdentityAttributePath: "",
				},
				b: Identity{
					IdentityAttributePath: "",
				},
			},
			true,
		},
		{
			"match on equal paths",
			args{
				a: Identity{
					IdentityAttributePath: "path",
				},
				b: Identity{
					IdentityAttributePath: "path",
				},
			},
			true,
		},
		{
			"no match on diffing paths",
			args{
				a: Identity{
					IdentityAttributePath: "path",
				},
				b: Identity{
					IdentityAttributePath: "different-path",
				},
			},
			false,
		},
		{
			"no match with same base but different subpath",
			args{
				a: Identity{
					IdentityAttributePath: "base/path",
				},
				b: Identity{
					IdentityAttributePath: "base/different-path",
				},
			},
			false,
		},
		{
			"match based on * pattern",
			args{
				a: Identity{
					IdentityAttributePath: "base/path",
				},
				b: Identity{
					IdentityAttributePath: "base/*",
				},
			},
			true,
		},
		{
			"no match based on * pattern but different subpath",
			args{
				a: Identity{
					IdentityAttributePath: "base/path/abc",
				},
				b: Identity{
					IdentityAttributePath: "base/*",
				},
			},
			false,
		},
		{
			"match based on * pattern but different subpath (explicit double *)",
			args{
				a: Identity{
					IdentityAttributePath: "base/path/abc",
				},
				b: Identity{
					IdentityAttributePath: "base/*/*",
				},
			},
			true,
		},
		{
			"no match based on * pattern but different subpath (explicit double * with no path)",
			args{
				a: Identity{
					IdentityAttributePath: "base/path/abc",
				},
				b: Identity{
					IdentityAttributePath: "base/**",
				},
			},
			false,
		},
		{
			"match based on * pattern in middle segment",
			args{
				a: Identity{
					IdentityAttributePath: "base/path/abc",
				},
				b: Identity{
					IdentityAttributePath: "base/*/abc",
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IdentityMatchesPath(tt.args.a, tt.args.b); got != tt.want {
				t.Errorf("IdentityMatchesPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_Match(t *testing.T) {

	type args struct {
		o        Identity
		matchers []IdentityMatcher
	}
	tests := []struct {
		name string
		i    Identity
		args args
		want bool
	}{
		{
			"empty",
			Identity{},
			args{
				o:        Identity{},
				matchers: nil,
			},
			true,
		},
		{
			"equality",
			Identity{
				"key": "value",
			},
			args{
				o: Identity{
					"key": "value",
				},
				matchers: nil,
			},
			true,
		},
		{
			"match based on * pattern",
			Identity{
				IdentityAttributePath: "base/path",
			},
			args{
				o: Identity{
					IdentityAttributePath: "base/*",
				},
			},
			true,
		},
		{
			"match based on * pattern but only with equality matcher",
			Identity{
				IdentityAttributePath: "base/path",
			},
			args{
				o: Identity{
					IdentityAttributePath: "base/*",
				},
				matchers: []IdentityMatcher{
					NewMatcher(IdentityEqual),
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
