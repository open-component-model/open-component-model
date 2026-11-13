package runtime

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"

	_ "embed"
)

// Typed is any object that is defined by a type that is versioned.
type Typed interface {
	// GetType returns the object's type
	GetType() Type
	SetType(Type)
	DeepCopyTyped() Typed
}

// Type represents a structured type with an optional version and a name.
// It is used to identify the type of an object in a versioned API.
// A Version is a specific iteration of the type,
// and Name is the name of the type.
type Type struct {
	Version string
	Name    string
}

// CompareTypesLexicographically compares two Types lexicographically based on their string representations.
func CompareTypesLexicographically(a, b Type) int {
	return strings.Compare(a.String(), b.String())
}

// NewUnversionedType creates a new Type instance without a version.
func NewUnversionedType(name string) Type {
	return Type{Name: name}
}

// NewVersionedType creates a new Type instance with a version.
func NewVersionedType(name, version string) Type {
	return Type{Name: name, Version: version}
}

// TypeFromString parses a type string in the formats:
// - "name" (unversioned)
// - "name/version" (versioned)
func TypeFromString(typ string) (Type, error) {
	parts := strings.Split(typ, "/")

	// Only allow one or two parts (name or name/version)
	if len(parts) > 2 {
		return Type{}, fmt.Errorf("invalid type %q, too many segments", typ)
	}

	var t Type
	if len(parts) == 1 {
		t = Type{Name: parts[0]} // Unversioned format
	} else {
		t = Type{Name: parts[0], Version: parts[1]} // Versioned format
	}

	// Validate name
	if t.Name == "" {
		return Type{}, fmt.Errorf("invalid type %q, missing name", typ)
	}

	return t, nil
}

// Equal checks if two Types are the same.
func (t Type) Equal(other Type) bool {
	return t.Name == other.Name && t.Version == other.Version
}

// String returns the formatted Type string.
// - Unversioned: "name"
// - Versioned: "name/version"
func (t Type) String() string {
	if t.Version != "" {
		return t.Name + "/" + t.Version
	}
	return t.Name // Unversioned type
}

// GetName returns the name of the type.
func (t Type) GetName() string {
	return t.Name
}

// GetVersion returns the version of the type.
func (t Type) GetVersion() string {
	return t.Version
}

// HasVersion checks if the type has a version associated with it.
func (t Type) HasVersion() bool {
	return t.Version != ""
}

// IsEmpty checks if the Type is empty (no version or name).
func (t Type) IsEmpty() bool {
	return t.Version == "" && t.Name == ""
}

// MarshalJSONTo writes the Type as a JSON string.
func (t Type) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(t.String()))
}

// UnmarshalJSONFrom parses a JSON string into Type. For convenience an object carrying
// a "type" member is accepted as well, so a whole typed object can be decoded into a Type.
func (t *Type) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var str string
	if dec.PeekKind() == '{' {
		var typed struct {
			Type string `json:"type"`
		}
		if err := json.UnmarshalDecode(dec, &typed); err != nil {
			return fmt.Errorf("could not unmarshal type: %w", err)
		}
		str = typed.Type
	} else if err := json.UnmarshalDecode(dec, &str); err != nil {
		return fmt.Errorf("could not unmarshal type: %w", err)
	}

	parsed, err := TypeFromString(str)
	if err != nil {
		return err
	}

	*t = parsed
	return nil
}

//go:embed schemas/Type.schema.json
var schemaType []byte

func (t Type) JSONSchema() []byte {
	return schemaType
}
