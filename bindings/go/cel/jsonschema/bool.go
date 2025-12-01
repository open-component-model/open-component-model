package jsonschema

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func celBool(pred bool) ref.Val {
	if pred {
		return types.True
	}
	return types.False
}
