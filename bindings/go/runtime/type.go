package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Typed is any object that is defined by a type that is versioned.
type Typed interface {
	// GetType returns the object's type
	GetType() Type
	DeepCopyTyped() Typed
}

// Type represents a structured type with an optional group, an optional version and a name.
// It is used to identify the type of an object in a versioned API.
// A Group is a namespace for related types,
// a Version is a specific iteration of the type,
// and Name is the name of the type.
type Type struct {
	Group   string
	Version string
	Name    string
}

// NewUngroupedVersionedType creates a new Type instance without a group and with a version.
func NewUngroupedVersionedType(name, version string) Type {
	return Type{Name: name, Version: version}
}

// NewUngroupedUnversionedType creates a new Type instance without a group and without a version.
func NewUngroupedUnversionedType(name string) Type {
	return Type{Name: name}
}

// NewType creates a new Type instance with a group and version.
func NewType(group, version, name string) Type {
	return Type{Group: group, Version: version, Name: name}
}

// TypeFromString parses a type string in the formats:
// - "name" (unversioned, no group)
// - "name/version" (versioned, no group)
// - "group.name" (unversioned, with group)
// - "group.name/version" (versioned, with group)
func TypeFromString(typ string) (Type, error) {
	parts := strings.Split(typ, "/")

	// Only allow one or two parts (name or name/version)
	if len(parts) > 2 {
		return Type{}, fmt.Errorf("invalid type %q, too many segments", typ)
	}

	var namePart, versionPart string
	if len(parts) == 1 {
		namePart = parts[0] // Unversioned format
	} else {
		namePart, versionPart = parts[0], parts[1] // Versioned format
	}

	// Split name part to extract group (if present)
	nameParts := strings.Split(namePart, ".")
	if len(nameParts) < 1 {
		return Type{}, fmt.Errorf("invalid type %q, missing name", typ)
	}

	// Extract Group and Name correctly
	var t Type
	if len(nameParts) == 1 {
		t = Type{Name: nameParts[0], Version: versionPart}
	} else {
		t = Type{
			Group:   strings.Join(nameParts[:len(nameParts)-1], "."),
			Name:    nameParts[len(nameParts)-1],
			Version: versionPart,
		}
	}

	// Validate fields
	if t.Name == "" {
		return Type{}, fmt.Errorf("invalid type %q, missing name", typ)
	}

	return t, nil
}

// Equal checks if two Types are the same.
func (t Type) Equal(other Type) bool {
	return t.Group == other.Group && t.Name == other.Name && t.Version == other.Version
}

// String returns the formatted Type string.
// - Unversioned: "name" or "group.name"
// - Versioned: "name/version" or "group.name/version"
func (t Type) String() string {
	namePart := t.Name
	if t.Group != "" {
		namePart = fmt.Sprintf("%s.%s", t.Group, t.Name)
	}
	if t.Version != "" {
		return fmt.Sprintf("%s/%s", namePart, t.Version)
	}
	return namePart // Unversioned type
}

// GetGroup returns the group of the type.
func (t Type) GetGroup() string {
	return t.Group
}

// GetName returns the name of the type.
func (t Type) GetName() string {
	return t.Name
}

// GetVersion returns the version of the type.
func (t Type) GetVersion() string {
	return t.Version
}

// HasGroup checks if the type has a group associated with it.
func (t Type) HasGroup() bool {
	return t.Group != ""
}

// HasVersion checks if the type has a version associated with it.
func (t Type) HasVersion() bool {
	return t.Version != ""
}

// MarshalJSON converts Type to a JSON string.
func (t Type) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON parses a JSON string into Type.
func (t *Type) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		var typed struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &typed); err != nil {
			return fmt.Errorf("could not unmarshal type: %w", err)
		}
		str = typed.Type
	}

	parsed, err := TypeFromString(str)
	if err != nil {
		return err
	}

	*t = parsed
	return nil
}
