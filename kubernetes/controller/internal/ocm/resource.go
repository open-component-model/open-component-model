package ocm

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

var ErrPluginNotFound = errors.New("digest processor plugin not found")

// VerifyResource verifies and processes the resource digest using the appropriate digest processor plugin.
func VerifyResource(ctx context.Context, pm *manager.PluginManager, resource *descriptor.Resource, cfg *configuration.Configuration) (*descriptor.Resource, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("processing resource digest")

	digestProcessor, err := pm.DigestProcessorRegistry.GetPlugin(ctx, resource.Access)
	if err != nil {
		// Return the resource along with the error to allow further handling if needed
		// (Currently, we just log the error and continue without digest verification because some resources may not
		// have digest processors yet)
		return resource, errors.Join(ErrPluginNotFound, err)
	}

	var creds map[string]string
	if cfg != nil {
		id, err := digestProcessor.GetResourceDigestProcessorCredentialConsumerIdentity(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("failed getting digest processor identity: %w", err)
		}

		credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: pm,
			Logger:        &logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed creating credential graph: %w", err)
		}

		creds, err = credGraph.Resolve(ctx, id)
		if err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("failed resolving credentials for digest processor: %w", err)
		}
	}

	// Process resource digest will also verify the digest if already present
	digestResource, err := digestProcessor.ProcessResourceDigest(ctx, resource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed processing resource digest: %w", err)
	}

	return digestResource, nil
}
