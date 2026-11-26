// Package componentlister holds helpers shared by the CLI commands that list
// components from a repository.
package componentlister

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// List lists the component names contained in the repository, resolving
// credentials through the credential graph so registries that need login are
// reachable. The names are lexically sorted, so the output is deterministic
// and matches the CTF lister regardless of the registry's own catalog order.
func List(ctx context.Context, pm *manager.PluginManager, credGraph credentials.Resolver, repository runtime.Typed) ([]string, error) {
	creds, err := ResolveCredentials(ctx, pm, credGraph, repository)
	if err != nil {
		return nil, err
	}

	lister, err := pm.ComponentListerRegistry.GetComponentLister(ctx, repository, creds)
	if err != nil {
		return nil, fmt.Errorf("could not get component lister for repository %+v: %w", repository, err)
	}

	var names []string
	if err := lister.ListComponents(ctx, "", func(page []string) error {
		names = append(names, page...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not list components in repository %+v: %w", repository, err)
	}

	slices.Sort(names)
	return names, nil
}

// ResolveCredentials resolves credentials for the component lister of the given
// repository through the credential graph.
//
// It returns nil credentials and no error when the repository needs none (the
// lister reports a nil consumer identity, as a CTF archive does) or when none
// are configured for the identity (ErrNotFound). A real failure, such as an
// invalid repository specification or a credential graph error, is returned to
// the caller so it is not mistaken for "no credentials".
func ResolveCredentials(ctx context.Context, pm *manager.PluginManager, credGraph credentials.Resolver, repository runtime.Typed) (runtime.Typed, error) {
	identity, err := pm.ComponentListerRegistry.GetComponentListerCredentialConsumerIdentity(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("could not determine credential consumer identity for repository %+v: %w", repository, err)
	}
	if identity == nil || credGraph == nil {
		return nil, nil
	}

	creds, err := credGraph.Resolve(ctx, identity)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolving credentials for repository %+v failed: %w", repository, err)
	}
	return creds, nil
}

// ComponentHint suggests a component reference when a repository reference very
// likely meant one. A repository reference whose path has a segment that looks
// like a component name (it contains a ".", for example "ocm.software/cli")
// usually needs a "//" to separate the repository from the component. It
// returns the suggested "<host>/<base>//<component>" reference, or "" when no
// suggestion applies. A reference that already has "//" yields "".
func ComponentHint(reference string) string {
	ref := reference
	if i := strings.Index(ref, "::"); i >= 0 {
		ref = ref[i+2:]
	}
	for _, scheme := range []string{"https://", "http://", "oci://"} {
		ref = strings.TrimPrefix(ref, scheme)
	}
	if strings.Contains(ref, "//") {
		return ""
	}

	segments := strings.Split(ref, "/")
	if len(segments) < 3 {
		return ""
	}
	// Skip the host (segment 0, which also contains dots) and look for the
	// first path segment that looks like a component name.
	for i := 1; i < len(segments); i++ {
		if strings.Contains(segments[i], ".") {
			base := strings.Join(segments[:i], "/")
			component := strings.Join(segments[i:], "/")
			return base + "//" + component
		}
	}
	return ""
}
