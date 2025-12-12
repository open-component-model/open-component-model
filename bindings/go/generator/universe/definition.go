package universe

import "strings"

const RuntimePackage = "ocm.software/open-component-model/bindings/go/runtime"

///////////////////////////////////////////////////////////////////////////////
// Def name generator for $defs
///////////////////////////////////////////////////////////////////////////////

// Definition generates an absolute, globally-unique $defs key:
//
//	ocm.software.open-component-model.bindings.go.runtime.Raw
//
// The convention is:
//
//	<pkgPath with "/" replaced by "."> + "." + <typeName>
//
// This is the canonical identity for schemas.
func Definition(key TypeKey) string {
	// Convert pkgPath from slashes → dots
	pkg := strings.ReplaceAll(key.PkgPath, "/", ".")
	return pkg + "." + key.TypeName
}

func IsRuntimeType(ti *TypeInfo) bool {
	return ti.Key.PkgPath == RuntimePackage && ti.Key.TypeName == "Type"
}

func IsRuntimeRaw(ti *TypeInfo) bool {
	return ti.Key.PkgPath == RuntimePackage && ti.Key.TypeName == "Raw"
}

func IsRuntimeTyped(ti *TypeInfo) bool {
	return ti.Key.PkgPath == RuntimePackage && ti.Key.TypeName == "Typed"
}
