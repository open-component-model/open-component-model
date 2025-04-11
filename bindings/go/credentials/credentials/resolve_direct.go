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

	// Leaf node: no outgoing edges, so use direct credentials.
	if len(vertex.Edges) == 0 {
		creds, ok := g.getFromCache(vertex.ID)
		if !ok {
			return nil, fmt.Errorf("failed to resolve credentials for node %q: no direct credentials found", vertex.ID)
		}
		return creds, nil
	}

	// Non–leaf node: recursively resolve each child and merge the results.
	node := identity.String()

	if credentials, cached := g.getFromCache(node); cached {
		return credentials, nil
	}

	result := make(map[string]string)
	if len(vertex.Edges) != 1 {
		return nil, fmt.Errorf("failed to resolve credentials for node %q: multiple outgoing edges that would be candidates: %v", vertex.ID, vertex.Edges)
	}
	for _, child := range vertex.Edges {
		childID := child[attributeIdentity].(runtime.Identity)
		res, err := g.resolveDirect(ctx, childID)
		if err != nil {
			return nil, err
		}
		plugin, err := g.getCredentialPlugin(ctx, childID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credentials for node %q: %w", childID, err)
		}

		// Let the plugin resolve the child's credentials.
		credentials, err := plugin.Resolve(childID, res)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credentials for node %q: %w", childID, err)
		}

		// Merge the resolved credentials into the result
		maps.Copy(result, credentials)
	}

	// Cache the resolved credentials for the identity
	g.addToCache(node, result)

	return result, nil
}
