// Package layout wires the --layout flag of commands that write component versions to the
// component version layout of OCI and CTF repository specifications.
package layout

import (
	"fmt"

	"github.com/spf13/pflag"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// FlagUsage describes the values of a layout flag.
const FlagUsage = "component version storage layout: v2 (manifest with the full descriptor) or normalized " +
	"(manifest with the normalized descriptor as its only layer; the manifest digest is stable across registries " +
	"and can be signed with cosign)"

// RegisterFlag registers an enum flag accepting the component version layouts.
// The flag has no default: callers apply it only if it was set (see FromFlag).
func RegisterFlag(flags *pflag.FlagSet, name, usage string) {
	enum.Var(flags, name, []string{string(layout.ComponentVersionLayoutV2), string(layout.ComponentVersionLayoutNormalized)}, usage)
	// The enum flag defaults to its first value, but an unset flag means "not chosen" here.
	flags.Lookup(name).DefValue = ""
}

// FromFlag returns the layout of a flag registered with RegisterFlag, and whether it was set.
func FromFlag(flags *pflag.FlagSet, name string) (layout.ComponentVersionLayout, bool, error) {
	if !flags.Changed(name) {
		return "", false, nil
	}
	value, err := enum.Get(flags, name)
	if err != nil {
		return "", false, fmt.Errorf("getting %s flag failed: %w", name, err)
	}
	return layout.ComponentVersionLayout(value), true, nil
}

// Apply sets the component version layout on an OCI or CTF repository spec.
// Other repository types have no layout and are rejected.
func Apply(spec runtime.Typed, l layout.ComponentVersionLayout) error {
	if err := l.Validate(); err != nil {
		return err
	}
	switch s := spec.(type) {
	case *ocirepospec.Repository:
		s.ComponentVersionLayout = l
	case *ctfrepospec.Repository:
		s.ComponentVersionLayout = l
	default:
		return fmt.Errorf("repository type %s does not support a component version layout", spec.GetType())
	}
	return nil
}
