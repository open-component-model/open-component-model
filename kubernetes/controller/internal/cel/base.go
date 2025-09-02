package cel

import (
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"

	ocmfunctions "ocm.software/open-component-model/kubernetes/controller/internal/cel/functions"
)

var BaseEnv = sync.OnceValues[*cel.Env, error](func() (*cel.Env, error) {
	return cel.NewEnv(
		ext.Lists(),
		ext.Sets(),
		ext.Strings(),
		ext.Math(),
		ext.Encoders(),
		ext.Bindings(),
		cel.OptionalTypes(),
		ocmfunctions.ToOCI(),
	)
})
