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
) *JSONSchemaDraft202012 {
	// baseline primitive (min/max/multipleOf/format/type)
	s := primitiveBaseForIdent(id)
	if s == nil {
		return nil
	}

	// apply type- and field-level markers
	typeMarkers := ExtractMarkerMap(ts, gd, BaseMarker)
	ApplyNumericMarkers(s, typeMarkers)
	ApplyEnumMarkers(s, typeMarkers)
	fieldMarkers := ExtractMarkerMapFromField(field, BaseMarker)
	ApplyNumericMarkers(s, fieldMarkers)
	ApplyEnumMarkers(s, fieldMarkers)

	return s
}

func primitiveBaseForIdent(ident *ast.Ident) *JSONSchemaDraft202012 {
	switch ident.Name {
	case "string":
		return &JSONSchemaDraft202012{Type: "string"}
	case "bool":
		return &JSONSchemaDraft202012{Type: "boolean"}
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
		return numberWithRange(math.MaxFloat32)
	case "float64":
		return numberWithRange(math.MaxFloat64)
	case "complex64", "complex128":
		return &JSONSchemaDraft202012{Type: "string"}
	default:
		return nil
	}
}

func numberWithRange(maximum float64) *JSONSchemaDraft202012 {
	return &JSONSchemaDraft202012{
		Type:    "number",
		Maximum: Ptr(maximum),
	}
}

func uintWithRange(maximum uint64) *JSONSchemaDraft202012 {
	return &JSONSchemaDraft202012{
		Type:    "integer",
		Minimum: Ptr(float64(0)),
		Maximum: Ptr(float64(maximum)),
	}
}

func intWithRange(minimum, maximum int64) *JSONSchemaDraft202012 {
	return &JSONSchemaDraft202012{
		Type:    "integer",
		Minimum: Ptr(float64(minimum)),
		Maximum: Ptr(float64(maximum)),
	}
}
