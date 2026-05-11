package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

	node := identity.String()

	// Non–leaf node: recursively resolve each child and merge the results.
	result := make(map[string]string)
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

		// Plugin interface still uses map[string]string — extract from the child's typed credential.
		childMap := typedToMap(childCredentials)

		// Let the plugin resolve the child's credentials.
		credentials, err := plugin.Resolve(ctx, childID, childMap)
		if err != nil {
			return nil, fmt.Errorf("no credentials for node %q resolved from plugin: %w", edgeID, err)
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
// For DirectCredentials, it returns the Properties map directly.
// For other typed credentials (e.g. HelmHTTPCredentials), it falls back to a JSON
// round-trip, extracting only string-valued fields (excluding the "type" field).
// This bridge exists because plugin interfaces and the legacy Resolve method work
// with map[string]string, while ResolveTyped returns runtime.Typed.
func typedToMap(cred runtime.Typed) map[string]string {
	if cred == nil {
		return nil
	}
	if dc, ok := cred.(*v1.DirectCredentials); ok {
		return maps.Clone(dc.Properties)
	}

	// Fallback: JSON round-trip for any typed credential.
	data, err := json.Marshal(cred)
	if err != nil {
		return nil
	}
	var rawAny map[string]any
	if err := json.Unmarshal(data, &rawAny); err != nil {
		return nil
	}
	result := make(map[string]string, len(rawAny))
	for k, v := range rawAny {
		if k == "type" {
			continue // Don't leak the type field into credential properties
		}
		s, ok := v.(string)
		if !ok {
			slog.Warn("typedToMap: skipping non-string credential field", "key", k)
			continue
		}
		if s != "" {
			result[k] = s
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
