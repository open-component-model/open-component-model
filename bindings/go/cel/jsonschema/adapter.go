package jsonschema

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

var _ types.Adapter = (*Adapter)(nil)

type Adapter struct {
}

func (a Adapter) NativeToValue(value any) ref.Val {
	//TODO implement me
	panic("implement me")
}
