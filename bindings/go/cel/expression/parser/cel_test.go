package parser

import (
	"testing"
)

func TestExtractExpressions(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:    "Simple expression",
			input:   "${resource.field}",
			want:    []string{"resource.field"},
			wantErr: false,
		},
		{
			name:    "Expression with function",
			input:   "${length(resource.list)}",
			want:    []string{"length(resource.list)"},
			wantErr: false,
		},
		{
			name:    "Expression with prefix",
			input:   "prefix-${resource.field}",
			want:    []string{"resource.field"},
			wantErr: false,
		},
		{
			name:    "Expression with suffix",
			input:   "${resource.field}-suffix",
			want:    []string{"resource.field"},
			wantErr: false,
		},
		{
			name:    "Multiple expressions",
			input:   "${resource1.field}-middle-${resource2.field}",
			want:    []string{"resource1.field", "resource2.field"},
			wantErr: false,
		},
		{
			name:    "Expression with map",
			input:   "${resource.map['key']}",
			want:    []string{"resource.map['key']"},
			wantErr: false,
		},
		{
			name:    "Expression with list index",
			input:   "${resource.list[0]}",
			want:    []string{"resource.list[0]"},
			wantErr: false,
		},
		{
			name:    "Complex expression",
			input:   "${resource.field == 'value' && resource.number > 5}",
			want:    []string{"resource.field == 'value' && resource.number > 5"},
			wantErr: false,
		},
		{
			name:    "No expressions",
			input:   "plain string",
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "Empty string",
			input:   "",
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "Incomplete expression",
			input:   "${incomplete",
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "Expression with escaped quotes",
			input:   "${resource.field == \"escaped\\\"quote\"}",
			want:    []string{"resource.field == \"escaped\\\"quote\""},
			wantErr: false,
		},
		{
			name:    "Multiple expressions with whitespace",
			input:   "  ${resource1.field}  ${resource2.field}  ",
			want:    []string{"resource1.field", "resource2.field"},
			wantErr: false,
		},
		{
			name:    "Expression with newlines",
			input:   "${resource.list.map(\n  x,\n  x * 2\n)}",
			want:    []string{"resource.list.map(\n  x,\n  x * 2\n)"},
			wantErr: false,
		},
		{
			name:    "Nested expression (should error)",
			input:   "${outer(${inner})} ${outer}",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Nested expression but with quotes",
			input:   "${outer(\"${inner}\")}",
			want:    []string{"outer(\"${inner}\")"},
			wantErr: false,
		},
		{
			name:    "Nested expression but with quotes (single quotes)",
			input:   "${outer('${inner}')}",
			want:    []string{"outer('${inner}')"},
			wantErr: false,
		},
		{
			name:    "Nested expression but with quotes (single quotes)",
			input:   "${outer('${inner}\")}",
			wantErr: true,
		},
		{
			name:    "Nested expression but with quotes (single quotes)",
			input:   "${outer(\"${inner}')}",
			wantErr: true,
		},
		{
			name:    "Nested closing brace without opening one",
			input:   "${\"text with }} inside\"}",
			want:    []string{"\"text with }} inside\""},
			wantErr: false,
		},
		{
			name:    "Nested open brace without closing one",
			input:   "${\"text with { inside\"}",
			want:    []string{"\"text with { inside\""},
			wantErr: false,
		},
		{
			name:    "Expressions with dictionary building",
			input:   "${true ? {'key': 'value'} : {'key': 'value2'}}",
			want:    []string{"true ? {'key': 'value'} : {'key': 'value2'}"},
			wantErr: false,
		},
		{
			name:  "Multiple expressions with dictionary building",
			input: "${true ? {'key': 'value'} : {'key': 'value2'}} somewhat ${resource.field} then ${false ? {'key': {'nestedKey':'value'}} : {'key': 'value2'}}",
			want: []string{
				"true ? {'key': 'value'} : {'key': 'value2'}",
				"resource.field",
				"false ? {'key': {'nestedKey':'value'}} : {'key': 'value2'}",
			},
			wantErr: false,
		},
		{
			name:    "Multiple incomplete expressions",
			input:   "${incomplete1 ${incomplete2",
			want:    []string{},
			wantErr: true,
		},
		{
			name:    "Mixed complete and incomplete",
			input:   "${complete} ${complete2} ${incomplete",
			want:    []string{"complete", "complete2"},
			wantErr: false,
		},
		{
			name:    "Mixed incomplete and complete",
			input:   "${incomplete ${complete}",
			want:    []string{},
			wantErr: true,
		},
		{
			name:    "Ternary with empty map literal",
			input:   "${condition ? value : {}}",
			want:    []string{"condition ? value : {}"},
			wantErr: false,
		},
		{
			name:    "Ternary with empty map on both sides",
			input:   "${condition ? {} : {}}",
			want:    []string{"condition ? {} : {}"},
			wantErr: false,
		},
		{
			name:    "Complex ternary with empty map (real world example)",
			input:   "${schema.spec.deployment.includeAnnotations ? schema.spec.deployment.annotations : {}}",
			want:    []string{"schema.spec.deployment.includeAnnotations ? schema.spec.deployment.annotations : {}"},
			wantErr: false,
		},
		{
			name:    "Ternary with has() and empty map",
			input:   "${has(schema.annotations) && includeAnnotations ? schema.annotations : {}}",
			want:    []string{"has(schema.annotations) && includeAnnotations ? schema.annotations : {}"},
			wantErr: false,
		},
		{
			name:    "Quoted nested expression with escaped quote before ${",
			input:   `${outer("test\"${inner}")}`,
			want:    []string{`outer("test\"${inner}")`},
			wantErr: false,
		},
		{
			name:    "${ at string start",
			input:   "${a}${b}",
			want:    []string{"a", "b"},
			wantErr: false,
		},
		{
			name:    "Expression touching another literal without whitespace",
			input:   "x${a}y${b}z",
			want:    []string{"a", "b"},
			wantErr: false,
		},
		{
			name:    "Whitespace inside ${ and before expression",
			input:   "${   resource.field   }",
			want:    []string{"   resource.field   "},
			wantErr: false,
		},
		{
			name:    "Brace-like structure inside a string literal",
			input:   "${\"not a { real brace } here\"}",
			want:    []string{"\"not a { real brace } here\""},
			wantErr: false,
		},
		{
			name:    "Escaped backslash before quote inside string",
			input:   `${"path\\\\to\\\\file"}`,
			want:    []string{`"path\\\\to\\\\file"`},
			wantErr: false,
		},
		{
			name:    "String literal ending right before nested ${ (allowed)",
			input:   `${outer("x"${inner})}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "String literal ending right before nested ${ with correct quotes",
			input:   `${outer("x\"${inner}")}`,
			want:    []string{`outer("x\"${inner}")`},
			wantErr: false,
		},
		{
			name:    "Unescaped ${ inside string literal",
			input:   `${outer("text ${ inner}")}`,
			want:    []string{`outer("text ${ inner}")`},
			wantErr: false,
		},
		{
			name:    "Escaped \\${ inside string literal (should NOT error)",
			input:   `${outer("text \\${ inner}")}`,
			want:    []string{`outer("text \\${ inner}")`},
			wantErr: false,
		},
		{
			name:    "Multiple back-to-back expressions without delimiter",
			input:   "${a}${b}${c}${d}",
			want:    []string{"a", "b", "c", "d"},
			wantErr: false,
		},
		{
			name:    "Expression containing } inside string literal",
			input:   `${"a}b"} more text`,
			want:    []string{`"a}b"`},
			wantErr: false,
		},
		{
			name:    "Expression containing { inside string literal",
			input:   `${"a{b"} more text`,
			want:    []string{`"a{b"`},
			wantErr: false,
		},
		{
			name:    "Expression containing both braces inside multiple string segments",
			input:   `${"a{b" + "c}d"}`,
			want:    []string{`"a{b" + "c}d"`},
			wantErr: false,
		},
		{
			name:    "Unclosed string literal inside expression (should error)",
			input:   `${"abc}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Escaped escaped quote inside string literal",
			input:   `${"abc\\\\\"def"}`,
			want:    []string{`"abc\\\\\"def"`},
			wantErr: false,
		},
		{
			name:    "Expression with CEL list literal",
			input:   `${[1, 2, 3, {"a":1}]}`,
			want:    []string{`[1, 2, 3, {"a":1}]`},
			wantErr: false,
		},
		{
			name:    "Expression with complex nested maps and strings",
			input:   `${{"a": {"b": "{\"c\": \"d\"}"}}}`,
			want:    []string{`{"a": {"b": "{\"c\": \"d\"}"}}`},
			wantErr: false,
		},
		{
			name:    "Nested ${ at start of input (should error because of missing string literal)",
			input:   `${""${inner}}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Nested ${ starting immediately after ${ (should error because of missing string literal)",
			input:   `${${inner}outer}`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractExpressions(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractExpressions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !equalStrings(got, tt.want) {
				t.Errorf("ExtractExpressions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, s := range a {
		if s != b[i] {
			return false
		}
	}
	return true
}

func TestIsOneShotExpression(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{"Simple one-shot", "${resource.field}", true, false},
		{"One-shot with function", "${length(resource.list)}", true, false},
		{"Not one-shot prefix", "prefix-${resource.field}", false, false},
		{"Not one-shot suffix", "${resource.field}-suffix", false, false},
		{"Not one-shot multiple", "${resource1.field}${resource2.field}", false, false},
		{"Not expression", "plain string", false, false},
		{"Empty string", "", false, false},
		{"Incomplete expression", "${incomplete", false, false},
		{"With map access", "${resource.map['key']}", true, false},
		{"With list index", "${resource.list[0]}", true, false},
		{"With escaped quotes", "${resource.field == \"escaped\\\"quote\"}", true, false},
		{"With newlines", "${resource.list.map(\n  x,\n  x * 2\n)}", true, false},
		{"Complex expression", "${resource.list.map(x, x.field).filter(y, y > 5)}", true, false},
		{"Nested expression (should error)", "${outer(${inner})}", false, true},
		{"Nested expression but with quotes", "${outer(\"${inner}\")}", true, false},
		{"Nested closing brace without opening one", "${\"text with }} inside\"}", true, false},
		{"Nested open brace without closing one", "${\"text with { inside\"}", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsStandaloneExpression(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("isOneShotExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("isOneShotExpression() = %v, want %v", got, tt.want)
			}
		})
	}
}
