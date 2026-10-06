package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	filev1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme(t *testing.T) {
	r := require.New(t)
	obj, err := Scheme.NewObject(GetGitResourceV1alpha1)
	r.NoError(err)
	r.IsType(&GetGitResource{}, obj)
	r.Equal("GetGitResource/v1alpha1", obj.GetType().String())
	step := &GetGitResource{
		Type: GetGitResourceV1alpha1, ID: "git",
		Spec:   &GetGitResourceSpec{Resource: &v2.Resource{Access: &runtime.Raw{Type: runtime.NewVersionedType("Git", "v1"), Data: []byte(`{"ref":"HEAD","repository":"https://example.com/repo","type":"Git/v1"}`)}}},
		Output: &GetGitResourceOutput{Resource: &v2.Resource{}, File: filev1alpha1.File{Type: runtime.NewVersionedType("File", "v1alpha1")}},
	}
	raw := &runtime.Raw{}
	r.NoError(Scheme.Convert(step, raw))
	var roundtrip GetGitResource
	r.NoError(Scheme.Convert(raw, &roundtrip))
	r.Equal(step, &roundtrip)
	copied := step.DeepCopy()
	copied.Spec.Resource.Access.Data[0] = '['
	copied.Output.Resource.Name = "changed"
	r.NotEqual(copied.Spec.Resource.Access.Data, step.Spec.Resource.Access.Data)
	r.Empty(step.Output.Resource.Name)
	for _, schema := range [][]byte{step.JSONSchema(), step.Spec.JSONSchema(), step.Output.JSONSchema()} {
		r.True(json.Valid(schema))
	}
	r.Contains(string(step.JSONSchema()), "GetGitResource/v1alpha1")
}
