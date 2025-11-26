package jsonschemagen

import (
	"go/ast"
	"math"
)

func newPrimitiveSchema(
	id *ast.Ident,
	ts *ast.TypeSpec,
	gd *ast.GenDecl,
	field *ast.Field,
) *Schema {
	// baseline primitive (min/max/multipleOf/format/type)
	s := primitiveBaseForIdent(id)
	if s == nil {
		return nil
	}

	// --- apply type-level markers: +ocm:jsonschema-gen:min=...,max=... ---
	typeMarkers := ExtractMarkerMap(ts, gd, BaseMarker)
	if len(typeMarkers) > 0 {
		ApplyNumericMarkers(s, typeMarkers)
	}

	// --- apply field-level markers (override type-level ones) ---
	if field != nil && field.Doc != nil {
		fieldMarkers := ExtractMarkerMapFromField(field.Doc, BaseMarker)
		if len(fieldMarkers) > 0 {
			ApplyNumericMarkers(s, fieldMarkers)
		}
	}

	return s
}

func primitiveBaseForIdent(ident *ast.Ident) *Schema {
	switch ident.Name {
	case "string":
		return &Schema{Type: "string"}
	case "bool":
		return &Schema{Type: "boolean"}
	// signed ints
	case "int":
		return intWithRange(math.MinInt, math.MaxInt)
	case "int8":
		return intWithRange(math.MinInt8, math.MaxInt8)
	case "int16":
		return intWithRange(math.MinInt16, math.MaxInt16)
	case "int32", "rune":
		return intWithRange(math.MinInt32, math.MaxInt32)
	case "int64":
		return intWithRange(math.MinInt64, math.MaxInt64)
	// unsigned ints
	case "uint":
		return uintWithRange(math.MaxUint)
	case "uint8", "byte":
		return uintWithRange(math.MaxUint8)
	case "uint16":
		return uintWithRange(math.MaxUint16)
	case "uint32":
		return uintWithRange(math.MaxUint32)
	case "uint64", "uintptr":
		return uintWithRange(math.MaxUint64)
	// floats, with IEEE-754 smallest representable normal step
	case "float32":
		return numberWithRange(math.MaxFloat32, "float")
	case "float64":
		return numberWithRange(math.MaxFloat64, "double")
	case "complex64", "complex128":
		return &Schema{Type: "string"}
	default:
		return nil
	}
}

func numberWithRange(maximum float64, format string) *Schema {
	return &Schema{
		Type:    "number",
		Maximum: Ptr(maximum),
		Format:  format,
	}
}

func uintWithRange(maximum uint64) *Schema {
	return &Schema{
		Type:    "integer",
		Minimum: Ptr(float64(0)),
		Maximum: Ptr(float64(maximum)),
	}
}

func intWithRange(minimum, maximum int64) *Schema {
	return &Schema{
		Type:    "integer",
		Minimum: Ptr(float64(minimum)),
		Maximum: Ptr(float64(maximum)),
	}
}
