package plugins

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type customType struct {
	Type            runtime.Type `json:"type"`
	AdditionalField string       `json:"additionalField"`
}

func (c *customType) GetType() runtime.Type {
	return c.Type
}

func (c *customType) SetType(t runtime.Type) {
	c.Type = t
}

func (c *customType) DeepCopyTyped() runtime.Typed {
	c2 := *c
	return &c2
}

var _ runtime.Typed = &customType{}

func TestGenerateJSONSchemaForType(t *testing.T) {
	type args struct {
		obj runtime.Typed
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "simple",
			args: args{
				obj: &customType{},
			},
			want:    []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins/custom-type","$ref":"#/$defs/customType","$defs":{"customType":{"properties":{"type":{"type":"string","pattern":"^([a-zA-Z0-9][a-zA-Z0-9.]*)(?:/(v[0-9]+(?:alpha[0-9]+|beta[0-9]+)?))?"},"additionalField":{"type":"string"}},"additionalProperties":false,"type":"object","required":["type","additionalField"]}}}`),
			wantErr: assert.NoError,
		},
		{
			name: "error for nil object",
			args: args{
				obj: nil,
			},
			wantErr: assert.Error,
		},
		{
			name: "error for nil raw",
			args: args{
				obj: &runtime.Raw{},
			},
			wantErr: assert.Error,
		},
		{
			name: "error for nil unstructured",
			args: args{
				obj: &runtime.Unstructured{},
			},
			wantErr: assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateJSONSchemaForType(tt.args.obj)
			if !tt.wantErr(t, err, fmt.Sprintf("GenerateJSONSchemaForType(%v)", tt.args.obj)) {
				return
			}
			assert.Equalf(t, string(tt.want), string(got), "GenerateJSONSchemaForType(%v)", tt.args.obj)
		})
	}
}
