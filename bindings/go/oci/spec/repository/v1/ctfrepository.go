package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ShortTypeCTF = "ctf"
	TypeCTF      = "CTFRepository"
)

// CTFRepository is a type that represents a CTF repository interpreted as per
// https://github.com/opencontainers/distribution-spec and backed by a CTF.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CTFRepository struct {
	Type runtime.Type `json:"type"`
	// Path is the local absolute or relative path to the CTF repository.
	Path string `json:"path"`
}

func (spec *CTFRepository) String() string {
	return spec.Path
}
