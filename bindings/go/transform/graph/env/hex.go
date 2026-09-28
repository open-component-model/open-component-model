package env

import (
	"encoding/hex"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// Hex returns a cel.EnvOption registering the "hex.encode" and "hex.decode"
// functions, mirroring the base64.encode / base64.decode functions from the CEL
// encoders extension.
//
//   - hex.decode(<string>) -> <bytes>   decodes a hex-encoded string into bytes
//   - hex.encode(<bytes>)  -> <string>  encodes bytes as a lower-case hex string
//
// hex.decode complements base64.encode for building RFC 9530 Content-Digest /
// Repr-Digest fields: OCM records digests as hex (resource.digest.value), while
// those fields carry the digest as base64. Chaining the two produces the correct
// value, e.g.
//
//	Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}']
//
// Invalid hex, or a wrong argument type, yields a CEL error that surfaces during
// expression evaluation.
func Hex() cel.EnvOption {
	return func(e *cel.Env) (*cel.Env, error) {
		for _, opt := range []cel.EnvOption{
			cel.Function("hex.decode",
				cel.Overload("hex_decode_string", []*cel.Type{cel.StringType}, cel.BytesType,
					cel.UnaryBinding(bindingHexDecode)),
			),
			cel.Function("hex.encode",
				cel.Overload("hex_encode_bytes", []*cel.Type{cel.BytesType}, cel.StringType,
					cel.UnaryBinding(bindingHexEncode)),
			),
		} {
			var err error
			if e, err = opt(e); err != nil {
				return nil, err
			}
		}
		return e, nil
	}
}

// bindingHexDecode is the implementation of the hex.decode function.
func bindingHexDecode(arg ref.Val) ref.Val {
	s, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("hex.decode() expects a string, got %T", arg.Value())
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return types.NewErr("hex.decode(%q): %s", s, err.Error())
	}
	return types.Bytes(decoded)
}

// bindingHexEncode is the implementation of the hex.encode function.
func bindingHexEncode(arg ref.Val) ref.Val {
	b, ok := arg.Value().([]byte)
	if !ok {
		return types.NewErr("hex.encode() expects bytes, got %T", arg.Value())
	}
	return types.String(hex.EncodeToString(b))
}
