package access

import (
	"context"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/wget/access/spec/v1"
)

const (
	WgetConsumerType = "wget"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	wget := &v1.Wget{}
	scheme.MustRegisterWithAlias(wget,
		runtime.NewVersionedType("wget", v1.Version),
		runtime.NewUnversionedType("wget"),
	)
}

// WgetAccess provides credential consumer identity resolution for wget access specs.
type WgetAccess struct{}

// NewWgetAccess creates a new WgetAccess instance.
func NewWgetAccess() *WgetAccess {
	return &WgetAccess{}
}

// GetResourceCredentialConsumerIdentity returns the consumer identity for the given resource
// if the resource access is a wget spec.
func (w *WgetAccess) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descruntime.Resource) (runtime.Identity, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	wget := v1.Wget{}
	if err := Scheme.Convert(resource.Access, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	identity, err := runtime.ParseURLToIdentity(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing wget URL to identity: %w", err)
	}

	identity.SetType(runtime.NewUnversionedType(WgetConsumerType))

	return identity, nil
}
