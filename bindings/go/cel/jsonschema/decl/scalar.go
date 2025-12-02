package decl

import (
	"math"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

const (
	MaxRequestSizeBytes = uint64(3 * 1024 * 1024)
	// DefaultMaxRequestSizeBytes is the size of the largest request that will be accepted
	DefaultMaxRequestSizeBytes = MaxRequestSizeBytes

	// MaxDurationSizeJSON
	// OpenAPI duration strings follow RFC 3339, section 5.6 - see the comment on maxDatetimeSizeJSON
	MaxDurationSizeJSON = 32
	// MaxDatetimeSizeJSON
	// OpenAPI datetime strings follow RFC 3339, section 5.6, and the longest possible
	// such string is 9999-12-31T23:59:59.999999999Z, which has length 30 - we add 2
	// to allow for quotation marks
	MaxDatetimeSizeJSON = 32
	// MinDurationSizeJSON
	// Golang allows a string of 0 to be parsed as a duration, so that plus 2 to account for
	// quotation marks makes 3
	MinDurationSizeJSON = 3
	// JSONDateSize is the size of a date serialized as part of a JSON object
	// RFC 3339 dates require YYYY-MM-DD, and then we add 2 to allow for quotation marks
	JSONDateSize = 12
	// MinDatetimeSizeJSON is the minimal length of a datetime formatted as RFC 3339
	// RFC 3339 datetimes require a full date (YYYY-MM-DD) and full time (HH:MM:SS), and we add 3 for
	// quotation marks like always in addition to the capital T that separates the date and time
	MinDatetimeSizeJSON = 21
	// MinStringSize is the size of literal ""
	MinStringSize = 2
	// MinBoolSize is the length of literal true
	MinBoolSize = 4
	// MinNumberSize is the length of literal 0
	MinNumberSize = 1

	// MaxFormatSize is the maximum size we allow for format strings
	MaxFormatSize          = 64
	MaxNameFormatRegexSize = 128

	NoMaxLength = math.MaxInt
)

// Scalar returns the scalar type declaration for the given CEL type name.
// If the type name does not correspond to a known scalar type, nil is returned.
func Scalar(typename string) *Type {
	switch typename {
	case BoolType.TypeName():
		return BoolType
	case BytesType.TypeName():
		return BytesType
	case DoubleType.TypeName():
		return DoubleType
	case DurationType.TypeName():
		return DurationType
	case IntType.TypeName():
		return IntType
	case NullType.TypeName():
		return NullType
	case StringType.TypeName():
		return StringType
	case TimestampType.TypeName():
		return TimestampType
	case UintType.TypeName():
		return UintType
	case ListType.TypeName():
		return ListType
	case MapType.TypeName():
		return MapType
	default:
		return nil
	}
}

var (
	// AnyType is equivalent to the CEL 'protobuf.Any' type in that the value may have any of the
	// types supported.
	AnyType = NewSimpleTypeWithMinSize("any", cel.AnyType, nil, 1)

	// BoolType is equivalent to the CEL 'bool' type.
	BoolType = NewSimpleTypeWithMinSize("bool", cel.BoolType, types.False, MinBoolSize)

	// BytesType is equivalent to the CEL 'bytes' type.
	BytesType = NewSimpleTypeWithMinSize("bytes", cel.BytesType, types.Bytes([]byte{}), MinStringSize)

	// DoubleType is equivalent to the CEL 'double' type which is a 64-bit floating point value.
	DoubleType = NewSimpleTypeWithMinSize("double", cel.DoubleType, types.Double(0), MinNumberSize)

	// DurationType is equivalent to the CEL 'duration' type.
	DurationType = NewSimpleTypeWithMinSize("duration", cel.DurationType, types.Duration{Duration: time.Duration(0)}, MinDurationSizeJSON)

	// DynType is the equivalent of the CEL 'dyn' concept which indicates that the type will be
	// determined at runtime rather than compile time.
	DynType = NewSimpleTypeWithMinSize("dyn", cel.DynType, nil, 1)

	// IntType is equivalent to the CEL 'int' type which is a 64-bit signed int.
	IntType = NewSimpleTypeWithMinSize("int", cel.IntType, types.IntZero, MinNumberSize)

	// NullType is equivalent to the CEL 'null_type'.
	NullType = NewSimpleTypeWithMinSize("null_type", cel.NullType, types.NullValue, 4)

	// StringType is equivalent to the CEL 'string' type which is expected to be a UTF-8 string.
	// StringType values may either be string literals or expression strings.
	StringType = NewSimpleTypeWithMinSize("string", cel.StringType, types.String(""), MinStringSize)

	// TimestampType corresponds to the well-known protobuf.Timestamp type supported within CEL.
	// Note that both the OpenAPI date and date-time types map onto TimestampType, so not all types
	// labeled as Timestamp will necessarily have the same MinSerializedSize.
	TimestampType = NewSimpleTypeWithMinSize("timestamp", cel.TimestampType, types.Timestamp{Time: time.Time{}}, JSONDateSize)

	// UintType is equivalent to the CEL 'uint' type.
	UintType = NewSimpleTypeWithMinSize("uint", cel.UintType, types.Uint(0), 1)

	// ListType is equivalent to the CEL 'list' type.
	ListType = NewListType(AnyType, NoMaxLength)

	// MapType is equivalent to the CEL 'map' type.
	MapType = NewMapType(AnyType, AnyType, NoMaxLength)
)

// NewListType returns a parameterized list type with a specified element type.
func NewListType(elem *Type, maxItems uint64) *Type {
	return &Type{
		name:         "list",
		celType:      cel.ListType(elem.CelType()),
		defaultValue: NewListValue(),
		ElemType:     elem,
		MaxElements:  maxItems,
		// a list can always be represented as [] in JSON, so hardcode the min size
		// to 2
		MinSerializedSize: 2,
	}
}

// NewMapType returns a parameterized map type with the given key and element types.
func NewMapType(key, elem *Type, maxProperties uint64) *Type {
	return &Type{
		name:         "map",
		celType:      cel.MapType(key.CelType(), elem.CelType()),
		defaultValue: NewMapValue(),
		KeyType:      key,
		ElemType:     elem,
		MaxElements:  maxProperties,
		// a map can always be represented as {} in JSON, so hardcode the min size
		// to 2
		MinSerializedSize: 2,
	}
}

// NewObjectType creates an object type with a qualified name and a set of field declarations.
func NewObjectType(name string, fields map[string]*Field) *Type {
	t := &Type{
		name:      name,
		celType:   cel.ObjectType(name),
		Fields:    fields,
		traitMask: traits.FieldTesterType | traits.IndexerType,
		// an object could potentially be larger than the min size we default to here ({}),
		// but we rely upon the caller to change MinSerializedSize accordingly if they add
		// properties to the object
		MinSerializedSize: 2,
	}
	t.defaultValue = NewObjectValue(t)
	return t
}

func NewSimpleTypeWithMinSize(name string, celType *cel.Type, zeroVal ref.Val, minSize uint64) *Type {
	return &Type{
		name:              name,
		celType:           celType,
		defaultValue:      zeroVal,
		MinSerializedSize: minSize,
	}
}
