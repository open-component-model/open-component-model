package universe

import (
	"go/ast"
)

///////////////////////////////////////////////////////////////////////////////
// Public Lookups
///////////////////////////////////////////////////////////////////////////////

// LookupType retrieves a type by full package path and name.
func (u *Universe) LookupType(pkgPath, typeName string) *TypeInfo {
	key := TypeKey{PkgPath: pkgPath, TypeName: typeName}
	return u.Types[key]
}

// ResolveIdent attempts to resolve an ident (Raw, LocalBlobAccess, etc.)
// either to a type in the same package, or to an imported alias to another package.
//
// This is required because fields may use:
//
//	runtime.Raw
//	Raw                  (alias)
//	type Raw = runtime.Raw
//	type Raw runtime.Raw
//	type Raw *runtime.Raw
func (u *Universe) ResolveIdent(filePath, pkgPath string, id *ast.Ident) (*TypeInfo, bool) {
	name := id.Name

	// 1. Same-package type?
	if ti, ok := u.Types[TypeKey{PkgPath: pkgPath, TypeName: name}]; ok {
		return ti, true
	}

	// 2. Look through imports for alias-based matches
	imports := u.ImportMaps[filePath]

	for alias, fullImportPath := range imports {
		// Case A:
		//   import runtime ".../runtime"
		//   GlobalAccess Raw
		//
		// Raw is not the alias, but Raw may be *declared* in the imported package.
		//
		if name == alias {
			// Not used for types, skip (alias is for package prefix)
			continue
		}

		// Case B:
		//   type Raw = runtime.Raw
		//   type Raw runtime.Raw
		//   type Raw *runtime.Raw
		//
		// For this, we need to look at *all* types in the imported package
		//
		for _, ti := range u.Types {
			if ti.Key.PkgPath != fullImportPath {
				continue
			}

			// If the imported package actually defines a type with this name:
			if ti.Key.TypeName == name {
				return ti, true
			}

			// The important part:
			// Check if the TypeSpec for this type *is an alias of SelectorExpr(alias.Raw)*.
			switch t := ti.TypeSpec.Type.(type) {
			case *ast.SelectorExpr:
				// match: type Raw = runtime.Raw
				if t.Sel.Name == name {
					return ti, true
				}

			case *ast.StarExpr:
				// match: type Raw = *runtime.Raw
				if sel, ok := t.X.(*ast.SelectorExpr); ok {
					if sel.Sel.Name == name {
						return ti, true
					}
				}
			}
		}
	}

	return nil, false
}

// ResolveSelector resolves a SelectorExpr like `foo.Bar` using the import map
// of the file that references it.
func (u *Universe) ResolveSelector(filePath string, sel *ast.SelectorExpr) (*TypeInfo, bool) {
	// prefix: package alias in the source file
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}

	alias := pkgIdent.Name

	// find full import path from file imports
	imports := u.ImportMaps[filePath]
	fullPkgPath, ok := imports[alias]
	if !ok {
		return nil, false
	}

	// match fully-qualified type in Universe
	for _, ti := range u.Types {
		if ti.Key.PkgPath == fullPkgPath && ti.Key.TypeName == sel.Sel.Name {
			return ti, true
		}
	}

	return nil, false
}
