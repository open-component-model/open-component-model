package env

import (
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// ContentDigestAlgorithmFunctionName is the name of the CEL function that maps an
// OCM digest algorithm name to its RFC 9530 Content-Digest / Repr-Digest key.
const ContentDigestAlgorithmFunctionName = "contentDigestAlgorithm"

// contentDigestAlgorithmKeys maps a normalized OCM digest algorithm name to the
// lower-case algorithm key registered for the HTTP Content-Digest / Repr-Digest
// fields (RFC 9530). Normalization upper-cases the input and strips separators, so
// "SHA-256", "sha256", and "SHA_256" all resolve to the same key. Note SHA-1 maps
// to the registered key "sha", not "sha-1".
var contentDigestAlgorithmKeys = map[string]string{
	"SHA256": "sha-256",
	"SHA512": "sha-512",
	"SHA1":   "sha",
	"MD5":    "md5",
	"CRC32C": "crc32c",
}

// ContentDigestAlgorithm returns a cel.EnvOption registering the global and member
// "contentDigestAlgorithm" function.
//
// The function maps an OCM digest algorithm name (as recorded on
// resource.digest.hashAlgorithm, e.g. "SHA-256") to the lower-case algorithm key
// used in the HTTP Content-Digest and Repr-Digest fields (RFC 9530, e.g. "sha-256"),
// so a header expression can build a syntactically correct field key:
//
//	Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=..."}']
//
// It is available both as a global call, contentDigestAlgorithm(<string>), and as a
// member call, <string>.contentDigestAlgorithm(). Input matching is
// case-and-separator-insensitive ("SHA-256", "sha256", "SHA_256" all map to
// "sha-256"). SHA-1 maps to the registered key "sha". An unrecognized algorithm
// yields a CEL error that surfaces during expression evaluation.
func ContentDigestAlgorithm() cel.EnvOption {
	return cel.Function(
		ContentDigestAlgorithmFunctionName,
		cel.Overload(
			"content_digest_algorithm_string",
			[]*cel.Type{cel.StringType},
			cel.StringType,
			cel.UnaryBinding(bindingContentDigestAlgorithm),
		),
		cel.MemberOverload(
			"string_content_digest_algorithm",
			[]*cel.Type{cel.StringType},
			cel.StringType,
			cel.UnaryBinding(bindingContentDigestAlgorithm),
		),
	)
}

// bindingContentDigestAlgorithm is the implementation of the
// contentDigestAlgorithm() CEL function.
func bindingContentDigestAlgorithm(arg ref.Val) ref.Val {
	name, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("contentDigestAlgorithm() expects a string, got %T", arg.Value())
	}
	key, ok := contentDigestAlgorithmKeys[normalizeDigestAlgorithm(name)]
	if !ok {
		return types.NewErr("contentDigestAlgorithm(%q): unknown digest algorithm", name)
	}
	return types.String(key)
}

// normalizeDigestAlgorithm upper-cases the algorithm name and removes the "-" and
// "_" separators so spelling variants normalize to a single lookup key.
func normalizeDigestAlgorithm(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	return name
}
