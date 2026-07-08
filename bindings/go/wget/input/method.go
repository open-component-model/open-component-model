package input

import (
	"context"
	"fmt"
	nethttp "net/http"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	"ocm.software/open-component-model/bindings/go/wget/internal/identity"
	"ocm.software/open-component-model/bindings/go/wget/repository"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	input "ocm.software/open-component-model/bindings/go/wget/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for wget-based inputs.
// It downloads a resource from an HTTP/S URL declared in the component constructor
// and returns it as a local blob to be stored in the component version.
type InputMethod struct {
	// HTTPConfig configures the HTTP client (timeouts, retries, TLS, routing) used for
	// downloads. When nil, a default client is used.
	HTTPConfig *httpv1alpha1.Config
	// MaxDownloadSize limits the number of bytes read from a response body. When zero,
	// DefaultMaxDownloadSize is used. A negative value disables the limit.
	MaxDownloadSize int64
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for a
// wget input from its URL, using the same wget consumer type as the access type so that
// credentials configured for a host resolve for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	return identity.CredentialConsumerIdentity(wget.URL)
}

// ProcessResource turns a wget input specification into a resource input method result.
//
// In the default local-blob mode it downloads the resource and returns it as local blob data.
// When the input sets Reference, it instead returns a processed resource
// carrying a wget access specification pointing at the URL, without downloading anything.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required in wget input spec")
	}

	// Access-spec mode: do not download. Store the resource with a wget access specification
	// pointing at the URL so the content is resolved lazily when the resource is accessed.
	if wget.Reference {
		remoteResource, err := i.createRemoteResourceAccess(resource, wget)
		if err != nil {
			return nil, fmt.Errorf("error creating remote resource access: %w", err)
		}
		return &constructor.ResourceInputMethodResult{
			ProcessedResource: constructorruntime.ConvertToDescriptorResource(remoteResource),
		}, nil
	}

	var client *nethttp.Client
	if i.HTTPConfig != nil {
		client = httpclient.New(httpclient.WithConfig(i.HTTPConfig))
	}

	maxDownloadSize := i.MaxDownloadSize
	switch {
	case maxDownloadSize == 0:
		maxDownloadSize = repository.DefaultMaxDownloadSize
	case maxDownloadSize < 0:
		maxDownloadSize = 0
	}

	data, err := download.Download(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	},
		download.WithClient(client),
		download.WithMaxDownloadSize(maxDownloadSize),
		download.WithCredentials(credentials),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading wget input from %q: %w", wget.URL, err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: data,
	}, nil
}

// createRemoteResourceAccess creates a resource with a wget access specification pointing at
// the input URL, mirroring the download request fields, so the content is fetched lazily
// instead of embedded as a local blob. The resource type set in the constructor is preserved.
func (i *InputMethod) createRemoteResourceAccess(resource *constructorruntime.Resource, wget v1.Wget) (*constructorruntime.Resource, error) {
	wgetAccess := &accessv1.Wget{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}

	if _, err := accessspec.Scheme.DefaultType(wgetAccess); err != nil {
		return nil, fmt.Errorf("error setting default type for wget access: %w", err)
	}
	resource.Access = wgetAccess

	return resource, nil
}
