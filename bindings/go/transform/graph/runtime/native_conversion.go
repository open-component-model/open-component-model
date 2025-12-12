package runtime

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// ErrUnsupportedType is returned when the type is not supported.
var ErrUnsupportedType = errors.New("unsupported type")

// GoNativeValue transforms CEL output into corresponding Go native type values.
func GoNativeValue(v ref.Val) (interface{}, error) {
	switch v.Type() {
	case types.BoolType:
		return v.Value().(bool), nil
	case types.IntType:
		return v.Value().(int64), nil
	case types.UintType:
		return v.Value().(uint64), nil
	case types.DoubleType:
		return v.Value().(float64), nil
	case types.StringType:
		return v.Value().(string), nil
	case types.ListType:
		return v.ConvertToNative(reflect.TypeOf([]interface{}{}))
	case types.MapType:
		return v.ConvertToNative(reflect.TypeOf(map[string]interface{}{}))
	case types.OptionalType:
		opt := v.(*types.Optional)
		if !opt.HasValue() {
			return nil, nil
		}
		return GoNativeValue(opt.GetValue())
	case types.NullType:
		return nil, nil
	default:
		// For types we can't convert, return as is with an error
		return v.Value(), fmt.Errorf("%w: %v", ErrUnsupportedType, v.Type())
	}
}
