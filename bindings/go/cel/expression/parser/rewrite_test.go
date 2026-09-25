package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
)

func TestRewriteIdentifier(t *testing.T) {
	const repl = `env.node[0]`
	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "bare identifier with field access",
			expr: `resource.access.url`,
			want: `env.node[0].access.url`,
		},
		{
			name: "identifier inside a string literal is preserved",
			expr: `"https://uploads.example/resource/" + resource.name`,
			want: `"https://uploads.example/resource/" + env.node[0].name`,
		},
		{
			name: "longer identifier is not matched",
			expr: `myresource.name`,
			want: `myresource.name`,
		},
		{
			name: "member-access field of the same name is not matched",
			expr: `x.resource`,
			want: `x.resource`,
		},
		{
			name: "identifier as a macro operand is rewritten, inner binder untouched",
			expr: `resource.labels.filter(l, l.name == "region")[0].value`,
			want: `env.node[0].labels.filter(l, l.name == "region")[0].value`,
		},
		{
			name: "multiple occurrences all rewritten",
			expr: `resource.name + "/" + resource.version`,
			want: `env.node[0].name + "/" + env.node[0].version`,
		},
		{
			name: "single-quoted literal is preserved",
			expr: `'resource' + resource.name`,
			want: `'resource' + env.node[0].name`,
		},
		{
			name: "escaped quote inside a literal keeps string tracking correct",
			expr: `"a\"resource\"b" + resource.name`,
			want: `"a\"resource\"b" + env.node[0].name`,
		},
		{
			name: "bare identifier alone",
			expr: `resource`,
			want: `env.node[0]`,
		},
		{
			name: "no occurrence is unchanged",
			expr: `"static-value"`,
			want: `"static-value"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parser.RewriteIdentifier(tc.expr, "resource", repl))
		})
	}
}
