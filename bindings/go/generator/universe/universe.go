package universe

import (
	"go/ast"
	"strings"
)

type TypeKey struct {
	PkgPath  string
	TypeName string
}

type TypeInfo struct {
	Key         TypeKey
	Struct      *ast.StructType
	File        *ast.File
	FilePath    string
	Comment     string
	IsAnnotated bool
}

type Universe struct {
	Types      map[TypeKey]*TypeInfo
	ImportMaps map[string]map[string]string
}

func New() *Universe {
	return &Universe{
		Types:      map[TypeKey]*TypeInfo{},
		ImportMaps: map[string]map[string]string{},
	}
}

// RecordImports registers the file’s imports.
//
// Should be called during file scanning:
//
//	u.RecordImports(filepath, file)
func (u *Universe) RecordImports(filePath string, f *ast.File) {
	aliasMap := map[string]string{}

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		var alias string

		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}

		aliasMap[alias] = path
	}

	u.ImportMaps[filePath] = aliasMap
}

func (u *Universe) AddType(pkgPath, typeName, filePath, comment string, st *ast.StructType, f *ast.File, annotated bool) {
	key := TypeKey{PkgPath: pkgPath, TypeName: typeName}

	u.Types[key] = &TypeInfo{
		Key:         key,
		Struct:      st,
		File:        f,
		FilePath:    filePath,
		Comment:     comment,
		IsAnnotated: annotated,
	}
}

func (u *Universe) AllAnnotated() []*TypeInfo {
	var out []*TypeInfo
	for _, ti := range u.Types {
		if ti.IsAnnotated {
			out = append(out, ti)
		}
	}
	return out
}
