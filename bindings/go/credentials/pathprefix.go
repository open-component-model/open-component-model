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

// consumerIdentities canonicalizes the config-authored identities of a consumer
// and expands the legacy pathprefix attribute into path patterns.
func (g *Graph) consumerIdentities(ctx context.Context, identities []runtime.Identity) []runtime.Identity {
	result := make([]runtime.Identity, 0, len(identities))
	for _, identity := range identities {
		result = append(result, pathPrefixToPath(ctx, g.canonicalizeConsumerIdentity(identity))...)
	}
	return result
}

// pathPrefixToPath converts the legacy pathprefix attribute into two identities, one
// with the prefix itself as path and one with the prefix followed by "/**", which
// together match the prefix and every path below it, as OCM v1 did. v1 compared
// paths literally, so glob metacharacters in the prefix are escaped.
// If the identity also sets path, path wins and pathprefix is dropped.
func pathPrefixToPath(ctx context.Context, identity runtime.Identity) []runtime.Identity {
	prefix, ok := identity[legacyIdentityAttributePathPrefix]
	if !ok {
		return []runtime.Identity{identity}
	}

	converted := identity.Clone()
	delete(converted, legacyIdentityAttributePathPrefix)

	if _, ok := converted[runtime.IdentityAttributePath]; ok {
		slog.WarnContext(ctx, "consumer identity sets both path and the legacy pathprefix, ignoring pathprefix",
			"identity", converted.String())
		return []runtime.Identity{converted}
	}

	// v1 trimmed the leading slash and matched every path for an empty prefix.
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		return []runtime.Identity{converted}
	}

	nested := converted.Clone()
	converted[runtime.IdentityAttributePath] = escapeGlob(prefix)
	nested[runtime.IdentityAttributePath] = escapeGlob(prefix) + "/**"

	slog.WarnContext(ctx, "consumer identity uses the legacy pathprefix attribute, replace it with path",
		"pathprefix", prefix, "path", []string{converted[runtime.IdentityAttributePath], nested[runtime.IdentityAttributePath]})

	return []runtime.Identity{converted, nested}
}

// escapeGlob escapes the metacharacters of the glob syntax used by runtime.IdentityMatchesPath.
func escapeGlob(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		switch c {
		case '\\', '*', '?', '[', ']', '{', '}':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}
