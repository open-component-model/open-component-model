package layout

import (
	"fmt"

	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Apply sets componentVersionLayout on a target repository spec (OCI or CTF) from a --layout flag
// value. "" and "v2" leave the spec unchanged (default). "normalized" sets the field. Any other
// value is an error. Specs of other types are left unchanged (the layout only applies to OCI/CTF).
func Apply(spec runtime.Typed, layout string) error {
	switch layout {
	case "", "v2":
		return nil
	case "normalized":
		switch s := spec.(type) {
		case *ocirepospec.Repository:
			s.ComponentVersionLayout = "normalized"
		case *ctfrepospec.Repository:
			s.ComponentVersionLayout = "normalized"
		}
		return nil
	default:
		return fmt.Errorf("unknown --layout %q (want v2 or normalized)", layout)
	}
}
