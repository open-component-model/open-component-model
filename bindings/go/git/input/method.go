// Package input implements the constructor input method for Git repositories.
// It archives a repository snapshot as a local blob in the component version.
package input

import (
	"context"
	"fmt"
	"reflect"

	"golang.org/x/crypto/ssh"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/git/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for Git inputs.
type InputMethod struct {
	// TempFolder holds temporary Git storage and the returned archive. When empty,
	// the OS temporary directory is used. The archive outlives ProcessResource
	// and is owned by the caller.
	TempFolder string
	// MaxArchiveSize caps compressed output, not the preceding clone or fetch.
	// Zero uses the default 1 GiB limit; a negative value disables the limit.
	MaxArchiveSize int64
	// CABundle extends system TLS trust.
	CABundle []byte
	// HostKeyCallback overrides SSH verification using the user's known_hosts.
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

	ref := spec.Ref
	if ref == "" && spec.Commit == "" {
		ref = "HEAD"
	}
	maxArchiveSize := i.MaxArchiveSize
	if maxArchiveSize == 0 {
		maxArchiveSize = download.DefaultMaxArchiveSize
	}
	result, err := download.Download(ctx, &accessv1.Git{
		Repository: spec.Repository,
		Ref:        ref,
		Commit:     spec.Commit,
	}, creds, download.Options{
		TempDir:         i.TempFolder,
		MaxArchiveSize:  maxArchiveSize,
		CABundle:        i.CABundle,
		HostKeyCallback: i.HostKeyCallback,
	})
	if err != nil {
		return nil, fmt.Errorf("error downloading git input: %w", err)
	}

	return &constructor.ResourceInputMethodResult{ProcessedBlobData: result.Blob}, nil
}

func (i *InputMethod) convertInput(resource *constructorruntime.Resource) (*v1.Git, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Input == nil || (reflect.ValueOf(resource.Input).Kind() == reflect.Pointer && reflect.ValueOf(resource.Input).IsNil()) {
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
