package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrSchemeRequired is returned when merging multiple typed credentials needs a
// scheme to serialize them but no CredentialTypeSchemeProvider was configured.
var ErrSchemeRequired = fmt.Errorf("a CredentialTypeSchemeProvider is required to merge multiple typed credentials")

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

	var resolved []runtime.Typed
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
		if credentials != nil {
			resolved = append(resolved, credentials)
		}
	}

	merged, err := mergeTyped(resolved, g.credentialTypeScheme())
	if err != nil {
		return nil, fmt.Errorf("merging credentials for node %q: %w", node, err)
	}

	g.setCredentials(node, merged)

	return merged, nil
}

// mergeTyped combines multiple resolved typed credentials into a single value,
// preserving the original map-merge semantic (later entries override earlier
// ones per field).
//
// The scheme must know every input's runtime type so scheme.Convert can
// serialize it to runtime.Raw. With no scheme, only single-result resolution
// works; merging returns ErrSchemeRequired.
func mergeTyped(creds []runtime.Typed, scheme *runtime.Scheme) (runtime.Typed, error) {
	switch len(creds) {
	case 0:
		return nil, nil
	case 1:
		return creds[0], nil
	}

	if scheme == nil {
		return nil, ErrSchemeRequired
	}

	merged := make(map[string]string)
	for _, c := range creds {
		// DirectCredentials nests its values under "properties", every other
		// typed credential carries its fields at the top level.
		if dc, ok := c.(*v1.DirectCredentials); ok {
			maps.Copy(merged, dc.Properties)
			continue
		}

		var raw runtime.Raw
		if err := scheme.Convert(c, &raw); err != nil {
			return nil, fmt.Errorf("converting credential of type %T to raw: %w", c, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw.Data, &fields); err != nil {
			return nil, fmt.Errorf("unmarshaling raw credential of type %s: %w", raw.Type, err)
		}
		for k, v := range fields {
			if k == "type" {
				continue
			}
			if s, ok := v.(string); ok && s != "" {
				merged[k] = s
			}
		}
	}

	return &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: merged,
	}, nil
}
