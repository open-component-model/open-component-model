package cel

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	ocmfunctions "ocm.software/open-component-model/kubernetes/controller/internal/cel/functions"
)

var sharedEnv = sync.OnceValues[*cel.Env, error](func() (*cel.Env, error) {
	return cel.NewEnv(
		ext.Lists(),
		ext.Sets(),
		ext.Strings(),
		ext.Math(),
		ext.Encoders(),
		ext.Bindings(),
		cel.OptionalTypes(),
	)
})

// ComponentInfoEnv constructs a CEL environment with a v1alpha1.ComponentInfo as a dependency.
// Extentions like `toOCI` need v1alpha1.ComponentInfo to properly provide an ImageReference from a localBlob.
func ComponentInfoEnv(component *v1alpha1.ComponentInfo) (*cel.Env, error) {
	if component == nil {
		return nil, fmt.Errorf("component info is nil but required to create the CEL environment")
	}

	env, err := sharedEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load shared cel environment: %w", err)
	}

	ociEnv, err := env.Extend(ocmfunctions.ToOCI(component))
	if err != nil {
		return nil, fmt.Errorf("failed to extend shared cel environment: %w", err)
	}

	return ociEnv, nil
}
