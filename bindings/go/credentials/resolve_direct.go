package credentials

import (
	"context"
	"fmt"
	"maps"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func (g *Graph) resolveDirect(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
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
	creds, ok := g.getCredentials(vertex.ID)
	if ok {
		return creds, nil
	}

	// Non–leaf node: recursively resolve each child and merge the results.
	node := identity.String()

	result := make(map[string]string)
	if len(vertex.Edges) != 1 {
		return nil, fmt.Errorf("failed to resolve credentials for node %q: multiple outgoing edges that would be candidates: %v", vertex.ID, vertex.Edges)
	}
	for id := range vertex.Edges {
		childID, ok := g.getIdentity(id)
		if !ok {
			return nil, fmt.Errorf("failed to resolve credentials for node %q: child node %q not found", vertex.ID, id)
		}
		childCredentials, err := g.resolveDirect(ctx, childID)
		if err != nil {
			return nil, err
		}
		plugin, err := g.getCredentialPlugin(ctx, childID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credentials for node %q: %w", childID, err)
		}

		// Let the plugin resolve the child's credentials.
		credentials, err := plugin.Resolve(ctx, childID, childCredentials)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credentials for node %q: %w", childID, err)
		}

		// Merge the resolved credentials into the result
		maps.Copy(result, credentials)
	}

	// Cache the resolved credentials for the identity
	g.setCredentials(node, result)

	return result, nil
}
