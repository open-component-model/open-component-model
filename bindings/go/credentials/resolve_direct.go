package credentials

import (
	"context"
	"fmt"
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolveFromGraph resolves credentials for a given identity by traversing the graph.
// Returns a runtime.Typed credential stored on the matching node.
func (g *Graph) resolveFromGraph(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	// Check for cancellation to exit early
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	vertex, err := g.matchAnyNode(identity)
	if err != nil {
		return nil, err
	}

	// Leaf node: return the credentials directly.
	creds, cached := g.getCredentials(vertex.ID)
	if cached {
		return creds, nil
	}

	// Non–leaf node: recursively resolve each child and merge the results.
	node := identity.String()

	result := make(map[string]string)
	for id := range vertex.Edges {
		childID, ok := g.getIdentity(id)
		if !ok {
			return nil, fmt.Errorf("no credentials for node %q available: child node %q not found", vertex.ID, id)
		}
		childCredentials, err := g.resolveFromGraph(ctx, childID)
		if err != nil {
			return nil, err
		}
		plugin, err := g.credentialPluginProvider.GetCredentialPlugin(ctx, childID)
		if err != nil {
			return nil, fmt.Errorf("could not get credential plugin for node %q: %w", childID, err)
		}

		// Extract map from child credentials for plugin resolution.
		childMap := typedToMap(childCredentials)

		// Let the plugin resolve the child's credentials.
		credentials, err := plugin.Resolve(ctx, childID, childMap)
		if err != nil {
			return nil, fmt.Errorf("no credentials for node %q resolved from plugin: %w", childID, err)
		}

		// Merge the resolved credentials into the result
		maps.Copy(result, credentials)
	}

	// Store as DirectCredentials
	typed := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: result,
	}

	// Cache the resolved credentials for the identity
	g.setCredentials(node, typed)

	return typed, nil
}

// typedToMap extracts map[string]string from a runtime.Typed credential.
// Used internally for plugin interfaces that still work with maps.
// TODO(matthiasbruns): Remove once plugin interfaces migrate to runtime.Typed https://github.com/open-component-model/ocm-project/issues/980
func typedToMap(cred runtime.Typed) map[string]string {
	if cred == nil {
		return nil
	}
	if dc, ok := cred.(*v1.DirectCredentials); ok {
		return dc.Properties
	}
	return nil
}
