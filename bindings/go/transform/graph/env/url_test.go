package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestURLFunction(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{
			name: "global path",
			expr: `"https://target.example/uploads" + url("https://src.example/a/b/c.tgz").path`,
			want: "https://target.example/uploads/a/b/c.tgz",
		},
		{
			name: "member path",
			expr: `"https://target.example" + "https://src.example/a/b".url().path`,
			want: "https://target.example/a/b",
		},
		{
			name: "host and scheme",
			expr: `url("https://src.example:8443/x").scheme + "://" + url("https://src.example:8443/x").host`,
			want: "https://src.example:8443",
		},
		{
			name: "hostname strips port",
			expr: `url("https://src.example:8443/x").hostname`,
			want: "src.example",
		},
		{
			name: "rawQuery preserved",
			expr: `url("https://src.example/x?token=abc&part=1").rawQuery`,
			want: "token=abc&part=1",
		},
		{
			name:    "non-string arg",
			expr:    `url(42).path`,
			wantErr: true,
		},
		{
			name:    "opaque uri rejected",
			expr:    `url("src.example:8443/a/b").path`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			b, err := NewEnvBuilder(map[string]interface{}{})
			r.NoError(err)
			celEnv, _, err := b.CurrentEnv()
			r.NoError(err)

			ast, iss := celEnv.Compile(tc.expr)
			if tc.wantErr {
				// Errors may surface at compile (type) or eval time depending on input.
				if iss != nil && iss.Err() != nil {
					return
				}
				prg, perr := celEnv.Program(ast)
				r.NoError(perr)
				_, _, evalErr := prg.Eval(map[string]interface{}{})
				r.Error(evalErr)
				return
			}

			r.NoError(iss.Err())
			prg, err := celEnv.Program(ast)
			r.NoError(err)
			out, _, err := prg.Eval(map[string]interface{}{})
			r.NoError(err)
			r.Equal(tc.want, out.Value())
		})
	}
}
