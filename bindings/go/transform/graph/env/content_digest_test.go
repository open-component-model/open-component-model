package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentDigestAlgorithmFunction(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{name: "sha-256 canonical", expr: `contentDigestAlgorithm("SHA-256")`, want: "sha-256"},
		{name: "sha-512", expr: `contentDigestAlgorithm("SHA-512")`, want: "sha-512"},
		{name: "sha-1 maps to sha", expr: `contentDigestAlgorithm("SHA-1")`, want: "sha"},
		{name: "md5", expr: `contentDigestAlgorithm("MD5")`, want: "md5"},
		{name: "separator/case insensitive", expr: `contentDigestAlgorithm("sha256")`, want: "sha-256"},
		{name: "underscore variant", expr: `contentDigestAlgorithm("SHA_512")`, want: "sha-512"},
		{name: "member call form", expr: `"SHA-256".contentDigestAlgorithm()`, want: "sha-256"},
		{
			name: "builds a Content-Digest field key",
			expr: `contentDigestAlgorithm("SHA-256") + "=:" + "deadbeef" + ":"`,
			want: "sha-256=:deadbeef:",
		},
		{name: "unknown algorithm errors", expr: `contentDigestAlgorithm("whirlpool")`, wantErr: true},
		{name: "non-string errors", expr: `contentDigestAlgorithm(7)`, wantErr: true},
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
