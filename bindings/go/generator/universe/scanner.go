package universe

import (
	"go/ast"
	"go/token"
	"path/filepath"
)

func (u *Universe) RegisterTypes(path string, file *ast.File, marker string) {
	pkgPath, _ := GuessPackagePath(filepath.Dir(path))

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}

			comment := ExtractStructComment(ts, gd)
			annotated := HasMarker(ts, gd, marker)

			u.AddType(pkgPath, ts.Name.Name, path, comment, st, file, annotated)
		}
	}
}
