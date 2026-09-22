package env

import (
	"net/url"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// URLFunctionName is the name of the CEL function that parses a URL string into
// its components.
const URLFunctionName = "url"

// URL returns a cel.EnvOption registering the global and member "url" function.
//
// The function parses a URL string into a map of its components so expressions
// can compose new URLs without hand-rolled string surgery. It is available both
// as a global call, url(<string>), and as a member call, <string>.url(). The
// returned map has string values for the keys:
//
//   - scheme    the URL scheme (e.g. "https")
//   - host      the host, optionally including the port (url.URL.Host)
//   - hostname  host without any port
//   - port      the port, or "" when absent
//   - path      the (decoded) path, e.g. "/a/b/c"
//   - rawPath   the escaped path when it differs from path, else path
//   - rawQuery  the query string without the leading "?"
//   - fragment  the fragment without the leading "#"
//   - user      the username in userinfo, or "" when absent
//
// A value that is not a valid URL, or not a string, yields a CEL error that
// surfaces during expression evaluation.
func URL() cel.EnvOption {
	return cel.Function(
		URLFunctionName,
		cel.Overload(
			"url_string",
			[]*cel.Type{cel.StringType},
			types.NewMapType(types.StringType, types.StringType),
			cel.UnaryBinding(bindingURL),
		),
		cel.MemberOverload(
			"string_url",
			[]*cel.Type{cel.StringType},
			types.NewMapType(types.StringType, types.StringType),
			cel.UnaryBinding(bindingURL),
		),
	)
}

// bindingURL is the implementation of the url() CEL function.
func bindingURL(arg ref.Val) ref.Val {
	raw, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("url() expects a string, got %T", arg.Value())
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return types.NewErr("url(%q): %s", raw, err.Error())
	}

	rawPath := parsed.EscapedPath()
	if rawPath == "" {
		rawPath = parsed.Path
	}

	result := map[string]string{
		"scheme":   parsed.Scheme,
		"host":     parsed.Host,
		"hostname": parsed.Hostname(),
		"port":     parsed.Port(),
		"path":     parsed.Path,
		"rawPath":  rawPath,
		"rawQuery": parsed.RawQuery,
		"fragment": parsed.Fragment,
		"user":     userName(parsed),
	}

	return types.DefaultTypeAdapter.NativeToValue(result)
}

func userName(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	return u.User.Username()
}
