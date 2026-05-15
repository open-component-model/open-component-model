package credentials

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolveFromGraph resolves credentials for a given identity by traversing the graph.
// Returns a runtime.Typed credential stored on the matching node.
func (g *Graph) resolveFromGraph(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	vertex, err := g.matchAnyNode(identity)
	if err != nil {
		return nil, err
	}

	creds, cached := g.getCredentials(vertex.ID)
	if cached {
		return creds, nil
	}

	node := identity.String()

	var resolved runtime.Typed
	for edgeID := range vertex.Edges {
		childID, ok := g.getIdentity(edgeID)
		if !ok {
			return nil, fmt.Errorf("no credentials for node %q available: child node %q not found", vertex.ID, edgeID)
		}
		childCredentials, err := g.resolveFromGraph(ctx, childID)
		if err != nil {
			return nil, err
		}
		plugin, err := g.credentialPluginProvider.GetCredentialPlugin(ctx, childID)
		if err != nil {
			return nil, fmt.Errorf("could not get credential plugin for node %q: %w", edgeID, err)
		}

		credentials, err := plugin.ResolveTyped(ctx, identity, childCredentials)
		if err != nil {
			return nil, fmt.Errorf("no credentials for node %q resolved from plugin: %w", edgeID, err)
		}

		// Last writer wins — the previous map-based path used maps.Copy, but
		// typed credentials cannot be merged generically.
		resolved = credentials
	}

	g.setCredentials(node, resolved)

	return resolved, nil
}
