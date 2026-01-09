package credentials

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolveFromGraph resolves credentials for a given identity by traversing the graph.
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

	// Non–leaf node: try to resolve each child and use first successfully resolved result.
	node := identity.String()

	var result runtime.Typed
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

		// Let the plugin resolve the child's credentials.
		credentials, err := plugin.Resolve(ctx, childID, childCredentials)
		if err != nil {
			return nil, fmt.Errorf("no credentials for node %q resolved from plugin: %w", childID, err)
		}

		// Merge the resolved credentials into the result
		result = credentials
		break
	}

	// Cache the resolved credentials for the identity
	g.setCredentials(node, result)

	return result, nil
}
