package universe

import (
	"go/ast"
	"go/token"
	"path/filepath"
)

func (u *Universe) RegisterConstsFromFile(path string, file *ast.File) {
	pkgPath, err := u.GuessPackagePath(filepath.Dir(path))
	if err != nil {
		return
	}

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}

		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range vs.Names {
				// must have explicit type for AST-only tracking
				if vs.Type == nil {
					continue
				}

				// Resolve enum type name
				switch t := vs.Type.(type) {
				case *ast.Ident:
					typeName := t.Name

					ti := u.Types[TypeKey{PkgPath: pkgPath, TypeName: typeName}]
					if ti == nil {
						continue
					}

					ti.Consts = append(ti.Consts, &ConstInfo{
						Name:    name.Name,
						Value:   safeValue(vs.Values, i),
						Doc:     vs.Doc,
						Comment: vs.Comment,
					})

				case *ast.SelectorExpr:
					// handle const Foo runtime.Type = "..." form
					pkgIdent, ok := t.X.(*ast.Ident)
					if !ok {
						continue
					}

					alias := pkgIdent.Name
					imports := u.ImportMaps[path]
					fullPkg, ok := imports[alias]
					if !ok {
						continue
					}

					typeName := t.Sel.Name
					ti := u.Types[TypeKey{PkgPath: fullPkg, TypeName: typeName}]
					if ti == nil {
						continue
					}

					ti.Consts = append(ti.Consts, &ConstInfo{
						Name:    name.Name,
						Value:   safeValue(vs.Values, i),
						Doc:     vs.Doc,
						Comment: vs.Comment,
					})
				}
			}
		}
	}
}

// safer value getter
func safeValue(vals []ast.Expr, i int) ast.Expr {
	if len(vals) == 0 || i >= len(vals) {
		return nil
	}
	return vals[i]
}
