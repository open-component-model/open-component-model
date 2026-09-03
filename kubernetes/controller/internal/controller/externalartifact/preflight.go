package externalartifact

import (
	"context"
	"fmt"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// CheckCRDInstalled verifies the Flux ExternalArtifact CRD is registered in the
// cluster before the controller starts watching it. The CRD is owned by Flux
// (installed by its source-controller); this controller only produces
// instances. Without it, the manager's cache would fail to establish the
// ExternalArtifact watch and crash on boot with an opaque error, so this
// returns an actionable message instead.
func CheckCRDInstalled(ctx context.Context, cfg *rest.Config) error {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	return checkCRDInstalledWith(ctx, dc)
}

// checkCRDInstalledWith is the discovery-client-driven core, split out for tests.
func checkCRDInstalledWith(_ context.Context, dc discovery.DiscoveryInterface) error {
	gv := sourcev1.GroupVersion.String()
	resources, err := dc.ServerResourcesForGroupVersion(gv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return crdMissingError(gv)
		}

		return fmt.Errorf("failed to discover %s resources: %w", gv, err)
	}

	for _, r := range resources.APIResources {
		if r.Kind == sourcev1.ExternalArtifactKind {
			return nil
		}
	}

	return crdMissingError(gv)
}

func crdMissingError(gv string) error {
	return fmt.Errorf(
		"the %s CRD %q is not installed: --enable-flux-external-artifacts-api requires the Flux "+
			"source-controller (which owns and installs the CRD, and whose kustomize/helm controllers consume the "+
			"artifacts). Install Flux, or disable the feature",
		gv, schema.GroupKind{Group: sourcev1.GroupVersion.Group, Kind: sourcev1.ExternalArtifactKind}.String(),
	)
}
