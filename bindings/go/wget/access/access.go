package access

import (
	"context"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	"ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

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
	if err := accessspec.Scheme.Convert(resource.Access, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return CredentialConsumerIdentity(wget.URL)
}

// CredentialConsumerIdentity resolves the credential consumer identity for the given
// wget URL. It is shared by the wget access type and the wget input method so that
// credentials configured for a host resolve identically whether the resource is
// declared via an access spec or an input spec.
func CredentialConsumerIdentity(url string) (runtime.Identity, error) {
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}

	identity, err := runtime.ParseURLToIdentity(url)
	if err != nil {
		return nil, fmt.Errorf("error parsing wget URL to identity: %w", err)
	}

	identity.SetType(accessspec.V1VersionedType)

	return identity, nil
}
