package v1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetComponentVersionRequest[T runtime.Typed] struct {
	// The Location of the Component Version
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`
}

type PostComponentVersionRequest[T runtime.Typed] struct {
	Repository T              `json:"repository"`
	Descriptor *v2.Descriptor `json:"descriptor"`
}

type GetLocalResourceRequest[T runtime.Typed] struct {
	// The Repository Specification where the Component Version is stored
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// Identity of the local resource
	Identity map[string]string `json:"identity,omitempty"`

	// The Location of the Local Resource to download to
	TargetLocation types.Location `json:"targetLocation"`
}

type PostLocalResourceRequest[T runtime.Typed] struct {
	// The Repository Specification where the Component Version should be stored
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// The ResourceLocation of the Local Resource
	ResourceLocation types.Location `json:"resourceLocation"`
	Resource         *v2.Resource   `json:"resource"`
}

type GetResourceRequest struct {
	types.Location
	// The resource specification to download
	*v2.Resource `json:"resource"`

	// The Location of the Local Resource to download to
	TargetLocation types.Location `json:"targetLocation"`
}

type PostResourceRequest struct {
	// The ResourceLocation of the Local Resource
	ResourceLocation types.Location `json:"resourceLocation"`
	Resource         *v2.Resource   `json:"resource"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}
