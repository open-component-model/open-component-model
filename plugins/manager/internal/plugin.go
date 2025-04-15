// Package internal contains a test plugin that is registered using internal
// functions of as a transfer plugin.
package internal

import (
	"context"
	"fmt"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/plugins/manager"
)

type MyInternalPlugin struct{}

func (m *MyInternalPlugin) Ping(ctx context.Context) error {
	return nil
}

func (m *MyInternalPlugin) GetComponentVersion(ctx context.Context, request manager.GetComponentVersionRequest, credentials manager.Attributes) (*descriptor.Descriptor, error) {
	_, _ = fmt.Fprintf(os.Stdout, "GetComponentVersion[%s %s]\n", request.Name, request.Version)

	return nil, nil
}

func (m *MyInternalPlugin) GetLocalResource(ctx context.Context, request manager.GetLocalResourceRequest, credentials manager.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "GetLocalResource[%s %s]\n", request.Name, request.Version)

	return nil
}

func (m *MyInternalPlugin) AddLocalResource(ctx context.Context, request manager.PostLocalResourceRequest, credentials manager.Attributes) (*descriptor.Resource, error) {
	_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource[%s %s]\n", request.Name, request.Version)

	return nil, nil
}

func (m *MyInternalPlugin) AddComponentVersion(ctx context.Context, request manager.PostComponentVersionRequest, credentials manager.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersion[%s %s]\n", request.Descriptor.Component.Name, request.Descriptor.Component.Version)

	return nil
}

var _ manager.ReadWriteRepositoryPluginContract = &MyInternalPlugin{}

func init() {
	typ := &v1.OCIImageLayer{
		Type: runtime.Type{
			Version: "OCIImageLayer",
			Name:    "v1",
		},
	}
	p := &MyInternalPlugin{}
	if err := manager.RegisterInternalTransferPlugin(p, []manager.Capability{
		{
			Capability: manager.ReadWriteComponentVersionRepositoryCapability,
			Type:       typ.GetType().String(),
		},
	}); err != nil {
		panic(err)
	}
}
