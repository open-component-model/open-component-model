package ctf

import (
	"encoding/json"
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type       = "CommonTransportFormat"
	ShortType  = "CTF"
	ShortType2 = "ctf"
)

// Repository is a type that represents an OCI repository backed by a CTF archive.
// This archive is accessed as if it was a remote OCI repository, but all accesses to it
// are translated into archive-specific operations.
//
// Note that content stored within this Repository is not necessarily globally accessible so
// the OCI library does not attempt to interpret global accesses.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Repository struct {
	// +ocm:jsonschema-gen:enum=CommonTransportFormat/v1,CTF/v1,ctf/v1
	// +ocm:jsonschema-gen:enum:deprecated=CommonTransportFormat,CTF,ctf
	Type runtime.Type `json:"type"`
	// Path is the path of the CTF Archive on the filesystem.
	//
	// Examples
	//   - ./relative/path/to/archive.tgz
	//   - relative/path/to/archive.tar
	//   - /absolute/path/to/archive-folder
	FilePath string `json:"filePath"`

	// AccessMode can be set to request readonly access or creation
	// The format of the path is determined by the access mode bitmask aggregated with |.
	// If not specified, the AccessMode will be interpreted as AccessModeReadOnly for read-based operations
	// and as AccessModeReadWrite for write-based operations.
	AccessMode AccessMode `json:"accessMode,omitempty"`
}

func (spec *Repository) String() string {
	return spec.FilePath
}

type AccessMode string

const (
	AccessModeReadOnly  = "readonly"
	AccessModeReadWrite = "readwrite"
	AccessModeCreate    = "create"
)

// UnmarshalJSON implements custom unmarshaling for AccessMode to support both
// string values ("readonly", "readwrite", "create") and numeric values (0, 1, 2).
// OCM v1 CTF implementation and spec has this field as byte. But we would like to
// keep the string representation for ease of usage.
func (mode *AccessMode) UnmarshalJSON(data []byte) error {
	var num int
	if err := json.Unmarshal(data, &num); err == nil {
		switch num {
		case 0:
			*mode = AccessModeReadOnly
		case 1:
			*mode = AccessModeReadWrite
		case 2:
			*mode = AccessModeCreate
		default:
			return fmt.Errorf("invalid AccessMode numeric value: %d", num)
		}
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*mode = AccessMode(str)
	return nil
}

// ToAccessBitmask converts the AccessMode string to a bitmask
// that can be used with the CTF library.
// The bitmask is a combination of the following flags:
// - O_RDONLY: Open for reading only
// - O_RDWR: Open for reading and writing
// - O_CREATE: Create the file if it does not exist
// The bitmask can use both AccessModeReadOnly and AccessModeReadWrite at the same time by
// using the | operator.
//
// Examples:
//   - AccessModeReadOnly -> ctf.O_RDONLY
//   - AccessModeReadWrite -> ctf.O_RDWR
//   - AccessModeCreate -> ctf.O_CREATE
//   - AccessModeReadWrite | AccessModeCreate -> ctf.O_RDONLY | ctf.O_CREATE
func (mode AccessMode) ToAccessBitmask() int {
	var base int
	split := strings.Split(string(mode), "|")
	for _, entry := range split {
		switch entry {
		case AccessModeReadOnly:
			base |= ctf.O_RDONLY
		case AccessModeReadWrite:
			base |= ctf.O_RDWR
		case AccessModeCreate:
			base |= ctf.O_CREATE
		}
	}
	return base
}
