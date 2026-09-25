package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHexFunctions(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    any
		wantErr bool
	}{
		{name: "decode to bytes", expr: `hex.decode("68656c6c6f")`, want: []byte("hello")},
		{name: "encode from bytes", expr: `hex.encode(b"hello")`, want: "68656c6c6f"},
		{name: "roundtrip", expr: `hex.encode(hex.decode("deadbeef"))`, want: "deadbeef"},
		{
			// hex digest -> RFC 9530 base64 value (SHA-256 of empty input).
			name: "hex digest to base64 content-digest value",
			expr: `base64.encode(hex.decode("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"))`,
			want: "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
		},
		{
			name: "full RFC 9530 Content-Digest field",
			expr: `contentDigestAlgorithm("SHA-256") + "=:" + base64.encode(hex.decode("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")) + ":"`,
			want: "sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:",
		},
		{name: "invalid hex errors", expr: `hex.decode("zz")`, wantErr: true},
		{name: "non-string decode errors", expr: `hex.decode(5)`, wantErr: true},
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
			switch want := tc.want.(type) {
			case []byte:
				r.Equal(want, out.Value())
			default:
				r.Equal(want, out.Value())
			}
		})
	}
}
