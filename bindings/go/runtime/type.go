package runtime

import (
	"fmt"
	"strings"
)

// Typed is any object that is defined by a type that is versioned.
type Typed interface {
	// GetType returns the objects type and version
	GetType() Type
}

type Type string

func NewType(base, version string) Type {
	return Type(fmt.Sprintf("%s/%s", base, version))
}

func Parse(typ string) (Type, error) {
	t := strings.Split(typ, "/")
	if len(t) != 2 {
		return "", fmt.Errorf("invalid type %q, not exactly type+version", typ)
	}
	if t[0] == "" {
		return "", fmt.Errorf("invalid type %q, missing type", typ)
	}
	if t[1] == "" {
		return "", fmt.Errorf("invalid type %q, missing version", typ)
	}
	return Type(fmt.Sprintf("%s/%s", t[0], t[1])), nil
}

func (t Type) Equal(other Type) bool {
	return t == other
}

func (t Type) String() string {
	return string(t)
}

func (t Type) GetType() Type {
	return t
}

func (t Type) GetKind() string {
	return strings.Split(string(t), "/")[0]
}

func (t Type) GetVersion() string {
	split := strings.Split(string(t), "/")
	if len(split) > 1 {
		return split[1]
	}
	return ""
}
