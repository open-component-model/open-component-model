package jsonschema

import (
	"fmt"
	"time"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func exprValue(value interface{}) (ref.Val, *DeclType, error) {
	switch v := value.(type) {
	case bool:
		return types.Bool(v), BoolType, nil
	case []byte:
		return types.Bytes(v), BytesType, nil
	case float64:
		return types.Double(v), DoubleType, nil
	case int64:
		return types.Int(v), IntType, nil
	case string:
		return types.String(v), StringType, nil
	case uint64:
		return types.Uint(v), UintType, nil
	case time.Duration:
		return types.Duration{Duration: v}, DurationType, nil
	case time.Time:
		return types.Timestamp{Time: v}, TimestampType, nil
	case types.Null:
		return v, NullType, nil
	case *ListValue:
		return v, ListType, nil
	case *MapValue:
		return v, MapType, nil
	case *ObjectValue:
		return v, v.objectType, nil
	default:
		return nil, unknownType, fmt.Errorf("unsupported type: (%T)%v", v, v)
	}
}
