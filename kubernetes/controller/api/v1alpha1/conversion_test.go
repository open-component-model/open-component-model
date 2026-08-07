package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

func TestTypesImplementHub(t *testing.T) {
	tests := []struct {
		name string
		obj  runtime.Object
	}{
		{name: "Component", obj: &Component{}},
		{name: "Deployer", obj: &Deployer{}},
		{name: "Repository", obj: &Repository{}},
		{name: "Resource", obj: &Resource{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub, ok := tt.obj.(conversion.Hub)
			if !ok {
				t.Fatalf("%s does not implement conversion.Hub", tt.name)
			}
			hub.Hub()
		})
	}
}
