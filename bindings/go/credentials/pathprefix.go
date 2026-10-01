package credentials

import (
	"context"
	"log/slog"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// legacyIdentityAttributePathPrefix is the OCM v1 attribute that scoped a consumer
// identity to every path below a prefix, compared segment by segment.
const legacyIdentityAttributePathPrefix = "pathprefix"

// pathPrefixToPath replaces the legacy pathprefix attribute with the path pattern
// "{prefix,prefix/**}", which matches the prefix and every path below it, as OCM v1 did.
// v1 compared paths literally, so glob metacharacters in the prefix are escaped.
// If the identity also sets path, path wins and pathprefix is dropped.
func pathPrefixToPath(ctx context.Context, identity runtime.Identity) runtime.Identity {
	prefix, ok := identity[legacyIdentityAttributePathPrefix]
	if !ok {
		return identity
	}

	converted := identity.Clone()
	delete(converted, legacyIdentityAttributePathPrefix)

	if _, ok := converted[runtime.IdentityAttributePath]; ok {
		slog.WarnContext(ctx, "consumer identity sets both path and the legacy pathprefix, ignoring pathprefix",
			"identity", converted.String())
		return converted
	}

	// v1 trimmed the leading slash and matched every path for an empty prefix.
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		return converted
	}

	escaped := escapeGlob(prefix)
	converted[runtime.IdentityAttributePath] = "{" + escaped + "," + escaped + "/**}"

	slog.WarnContext(ctx, "consumer identity uses the legacy pathprefix attribute, replace it with path",
		"pathprefix", prefix, "path", converted[runtime.IdentityAttributePath])

	return converted
}

// escapeGlob escapes the metacharacters of the glob syntax used by runtime.IdentityMatchesPath.
func escapeGlob(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		switch c {
		case '\\', '*', '?', '[', ']', '{', '}', ',':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}
