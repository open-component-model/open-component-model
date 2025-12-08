package jsonschema

import "regexp"

// ReservedSymbols is a list of RESERVED symbols defined in the CEL lexer.
// No identifiers are allowed to collide with these symbols.
// https://github.com/google/cel-spec/blob/master/doc/langdef.md#syntax
var ReservedSymbols = newStringSet(
	"true", "false", "null", "in",
	"as", "break", "const", "continue", "else",
	"for", "function", "if", "import", "let",
	"loop", "package", "namespace", "return", // !! 'namespace' is used heavily in Kubernetes
	"var", "void", "while",
)

type stringSet map[string]struct{}

func (s stringSet) Has(item string) bool {
	_, found := s[item]
	return found
}

func newStringSet(items ...string) stringSet {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

// Escape escapes ident and returns a CEL identifier (of the form '[a-zA-Z_][a-zA-Z0-9_]*'), or returns
// false if the ident does not match the supported input format of `[a-zA-Z_.-/][a-zA-Z0-9_.-/]*`.
// Escaping Rules:
//   - '__' escapes to '__underscores__'
//   - '.' escapes to '__dot__'
//   - '-' escapes to '__dash__'
//   - '/' escapes to '__slash__'
//   - Identifiers that exactly match a CEL RESERVED keyword escape to '__{keyword}__'. The keywords are: "true", "false",
//     "null", "in", "as", "break", "const", "continue", "else", "for", "function", "if", "import", "let", loop", "package",
//     "namespace", "return".
func Escape(ident string) (string, bool) {
	if len(ident) == 0 || ('0' <= ident[0] && ident[0] <= '9') {
		return "", false
	}
	if ReservedSymbols.Has(ident) {
		return "__" + ident + "__", true
	}

	escapeCheck := skipRegexCheck(ident)
	if escapeCheck.invalidCharFound {
		return "", false
	}
	if escapeCheck.canSkipRegex {
		return ident, true
	}

	ok := true
	ident = expandMatcher.ReplaceAllStringFunc(ident, func(s string) string {
		switch s {
		case "__":
			return "__underscores__"
		case ".":
			return "__dot__"
		case "-":
			return "__dash__"
		case "/":
			return "__slash__"
		default: // matched an unsupported supported
			ok = false
			return ""
		}
	})
	if !ok {
		return "", false
	}
	return ident, true
}

// expandMatcher matches the escape sequence, characters that are escaped, and characters that are unsupported
var expandMatcher = regexp.MustCompile(`(__|[-./]|[^a-zA-Z0-9-./_])`)

// validCharacterFilter indicates the allowed characters.
var validCharacterFilter = newCharacterFilter("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_")

type escapeCheck struct {
	canSkipRegex     bool
	invalidCharFound bool
}

// skipRegexCheck checks if escape would be skipped.
// if invalidCharFound is true, it must have invalid character; if invalidCharFound is false, not sure if it has invalid character or not
func skipRegexCheck(ident string) escapeCheck {
	escapeCheck := escapeCheck{canSkipRegex: true, invalidCharFound: false}
	// skip escape if possible
	previousUnderscore := false
	for _, c := range ident {
		if c == '/' || c == '-' || c == '.' {
			escapeCheck.canSkipRegex = false
			return escapeCheck
		}
		intc := int(c)
		if intc < 0 || intc >= len(validCharacterFilter) || !validCharacterFilter[intc] {
			escapeCheck.invalidCharFound = true
			return escapeCheck
		}
		if c == '_' && previousUnderscore {
			escapeCheck.canSkipRegex = false
			return escapeCheck
		}

		previousUnderscore = c == '_'
	}
	return escapeCheck
}

// newCharacterFilter returns a boolean array to indicate the allowed characters
func newCharacterFilter(characters string) []bool {
	maxChar := 0
	for _, c := range characters {
		if maxChar < int(c) {
			maxChar = int(c)
		}
	}
	filter := make([]bool, maxChar+1)

	for _, c := range characters {
		filter[int(c)] = true
	}

	return filter
}
