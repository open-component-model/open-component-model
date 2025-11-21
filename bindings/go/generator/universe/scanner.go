package universe

import (
	"go/ast"
	"go/token"
	"path/filepath"
)

func (u *Universe) RegisterTypes(path string, file *ast.File) {
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

			u.AddType(pkgPath, ts.Name.Name, path, file, ts, gd)
		}
	}
}
