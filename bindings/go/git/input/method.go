// Package input implements the constructor input method for Git repositories.
// It archives a repository snapshot as a local blob in the component version.
package input

import (
	"context"
	"fmt"

	"golang.org/x/crypto/ssh"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	gitcreds "ocm.software/open-component-model/bindings/go/git/spec/credentials"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/git/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for Git inputs.
type InputMethod struct {
	// TempFolder holds temporary Git storage and the returned archive. When empty,
	// the OS temporary directory is used. The archive outlives ProcessResource
	// and is owned by the caller.
	TempFolder string
	// HTTPConfig configures the HTTP client that serves http(s) repositories.
	// Nil uses the shared client's defaults.
	HTTPConfig *httpv1alpha1.Config
	// MaxArchiveSize caps compressed output, not the preceding clone or fetch.
	// Nil uses the download package default, which is unlimited. Non-positive
	// values disable the limit; output is streamed to disk.
	MaxArchiveSize *int64
	// HostKeyCallback overrides SSH host key verification. If nil, verification
	// uses the user's known_hosts.
	HostKeyCallback ssh.HostKeyCallback
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity uses the same identity as Git access,
// so repository credentials resolve for both access and input.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	spec, err := i.convertInput(resource)
	if err != nil {
		return nil, err
	}

	return identityv1.IdentityFromURL(spec.Repository)
}

// ProcessResource returns the selected repository snapshot as a gzip-compressed tar.
// ProcessedBlobData implements io.Closer. Its consumer must close the blob after
// closing all readers to remove the archive; closing a reader alone retains it.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	spec, err := i.convertInput(resource)
	if err != nil {
		return nil, err
	}

	var creds *credsv1.GitCredentials
	if credentials != nil {
		creds, err = credsv1.ConvertToGitCredentials(credentials)
		if err != nil {
			return nil, err
		}
	}

	opts := download.Options{
		TempDir:         i.TempFolder,
		MaxArchiveSize:  download.DefaultMaxArchiveSize,
		HostKeyCallback: i.HostKeyCallback,
	}
	if i.MaxArchiveSize != nil {
		opts.MaxArchiveSize = *i.MaxArchiveSize
	}
	if i.HTTPConfig != nil {
		opts.HTTPClient = httpclient.New(httpclient.WithConfig(i.HTTPConfig))
	}

	ref := spec.Ref
	if ref == "" && spec.Commit == "" {
		ref = "HEAD"
	}
	result, err := download.Download(ctx, &accessv1.Git{
		Repository: spec.Repository,
		Ref:        ref,
		Commit:     spec.Commit,
	}, creds, opts)
	if err != nil {
		return nil, fmt.Errorf("error downloading git input: %w", err)
	}

	return &constructor.ResourceInputMethodResult{ProcessedBlobData: result.Blob}, nil
}

func (i *InputMethod) convertInput(resource *constructorruntime.Resource) (*v1.Git, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Input == nil {
		return nil, fmt.Errorf("resource input is required")
	}

	spec := &v1.Git{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid git input spec: %w", err)
	}

	return spec, nil
}

func (i *InputMethod) GetCredentialTypeScheme() *runtime.Scheme {
	return gitcreds.Scheme
}
