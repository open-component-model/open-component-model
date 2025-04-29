package ctf

import (
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
type Repository struct {
	Type runtime.Type `json:"type"`
	// Path is the path of the CTF Archive on the filesystem.
	//
	// Examples
	//   - ./relative/path/to/archive.tgz
	//   - relative/path/to/archive.tar
	//   - /absolute/path/to/archive-folder
	Path string `json:"path"`

	// AccessMode can be set to request readonly access or creation
	// The format of the path is determined by the access mode bitmask aggregated with |.
	// If not specified, the AccessMode will be interpreted as AccessModeReadOnly for read-based operations
	// and as AccessModeReadWrite for write-based operations.
	AccessMode AccessMode `json:"accessMode,omitempty"`
}

func (spec *Repository) String() string {
	return spec.Path
}

type AccessMode string

const (
	AccessModeReadOnly  = "readonly"
	AccessModeReadWrite = "readwrite"
	AccessModeCreate    = "create"
)

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
