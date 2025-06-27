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

type ListComponentVersionsRequest[T runtime.Typed] struct {
	// The Location of the Component Version
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
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
}

type GetLocalSourceRequest[T runtime.Typed] struct {
	// The Repository Specification where the Component Version is stored
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// Identity of the local resource
	Identity map[string]string `json:"identity,omitempty"`
}

type GetLocalResourceResponse struct {
	// Location where the local resource will be downloaded to and can be accessed.
	Location types.Location `json:"location"`
	Resource *v2.Resource   `json:"resource"`
}

type GetLocalSourceResponse struct {
	// Location where the local source will be downloaded to and can be accessed.
	Location types.Location `json:"location"`
	Source   *v2.Source     `json:"source"`
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

type PostLocalSourceRequest[T runtime.Typed] struct {
	// The Repository Specification where the Component Version should be stored
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// The SourceLocation of the Local Source
	SourceLocation types.Location `json:"sourceLocation"`
	Source         *v2.Source     `json:"source"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}
type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}
