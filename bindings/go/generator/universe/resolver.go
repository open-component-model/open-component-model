package universe

import (
	"go/ast"
	"path/filepath"
	"strings"
)

///////////////////////////////////////////////////////////////////////////////
// Public Lookups
///////////////////////////////////////////////////////////////////////////////

// LookupType retrieves a type by full package path and name.
func (u *Universe) LookupType(pkgPath, typeName string) *TypeInfo {
	key := TypeKey{PkgPath: pkgPath, TypeName: typeName}
	return u.Types[key]
}

// ResolveIdent resolves an unqualified identifier (Ident) within
// the same package as the file where it appears.
func (u *Universe) ResolveIdent(filePath string, pkgPath string, ident *ast.Ident) (*TypeInfo, bool) {
	ti := u.LookupType(pkgPath, ident.Name)
	if ti == nil {
		return nil, false
	}
	return ti, true
}

// ResolveSelector resolves a SelectorExpr like `foo.Bar` using the import map
// of the file that references it.
func (u *Universe) ResolveSelector(filePath string, sel *ast.SelectorExpr) (*TypeInfo, bool) {
	aliasIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}

	alias := aliasIdent.Name

	imports := u.ImportMaps[filePath]
	pkgPath := imports[alias]
	if pkgPath == "" {
		return nil, false
	}

	ti := u.LookupType(pkgPath, sel.Sel.Name)
	if ti == nil {
		return nil, false
	}

	return ti, true
}

///////////////////////////////////////////////////////////////////////////////
// Def name generator for $defs
///////////////////////////////////////////////////////////////////////////////

// DefName returns a canonical, collision-free name for a type inside $defs.
// Example:
//
//	"github.com/foo/bar", "Baz"
//	→ "github.com.foo.bar.Baz"
func DefName(key TypeKey) string {
	ns := strings.ReplaceAll(key.PkgPath, "/", ".")
	return ns + "." + key.TypeName
}

///////////////////////////////////////////////////////////////////////////////
// Convenience
///////////////////////////////////////////////////////////////////////////////

// FileDir returns the directory of a TypeInfo for locating associated files.
func (ti *TypeInfo) FileDir() string {
	return filepath.Dir(ti.FilePath)
}
