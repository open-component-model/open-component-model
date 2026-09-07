// Package sbom is a reusable set of functions that a command can use to
// discover SBOMs for a given reference.
//
// There are two strategies currently:
//
//  1. "ocm.software/artifact-references" label resolution to find SBOMs pointing
//     at the given resource.
//  2. For OCI Artifacts, we offer discovering and listing SBOMs attached and generated
//     via "docker buildx build --sbom=true".
package sbom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	ocmblob "ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artifactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artifactref/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceTypeSBOM is the resource type an SBOM is published under.
const ResourceTypeSBOM = "sbom"

const (
	attributeOS           = "os"
	attributeArchitecture = "architecture"
	attributeVariant      = "variant"
)

// errNoReference signals that no resource of the component version describes the
// target. This is an internal error only.
var errNoReference = errors.New("no resource references the target")

// Downloader reads the raw bytes of a resource. This is taken as to avoid
// dependencies on cmd.
type Downloader func(ctx context.Context, res *descriptor.Resource, identity runtime.Identity) (ocmblob.ReadOnlyBlob, error)

// Request has everything Discover needs to look for the SBOMs of one resource.
type Request struct {
	Descriptor    *descriptor.Descriptor
	Resource      *descriptor.Resource
	PluginManager *manager.PluginManager
	Credentials   credentials.Resolver
	Download      Downloader
	Logger        *slog.Logger
	// Options are passed to sbom discovery, for example WithAllSBOMPlatforms.
	Options []repository.SBOMOption
}

// Discover returns every SBOM describing the requested resource. The first strategy to
// produce anything wins.
func Discover(ctx context.Context, req Request) ([]repository.SBOM, error) {
	sboms, err := fromArtifactReferences(ctx, req)
	if err == nil {
		return sboms, nil
	}
	if !errors.Is(err, errNoReference) {
		return nil, err
	}

	req.Logger.Debug("no sbom resource references the requested resource, inspecting its artifact",
		slog.String("resource", req.Resource.ToIdentity().String()))

	return fromAttestations(ctx, req)
}

// fromArtifactReferences collects every resource of the component version declaring,
// through the artifact reference label, that it describes the target.
func fromArtifactReferences(ctx context.Context, req Request) ([]repository.SBOM, error) {
	targetIdentity := req.Resource.ToIdentity()

	referring, err := artifactref.FindDescribingResources(req.Descriptor, targetIdentity)
	if err != nil && !errors.Is(err, artifactref.ErrNotFound) {
		return nil, err
	}

	var describing []*descriptor.Resource
	for _, candidate := range referring {
		if candidate.Type != ResourceTypeSBOM {
			req.Logger.Debug("ignoring resource referencing the requested resource, it is not an sbom",
				slog.String("resource", candidate.ToIdentity().String()),
				slog.String("type", candidate.Type))
			continue
		}
		describing = append(describing, candidate)
	}

	if len(describing) == 0 {
		return nil, fmt.Errorf("%w: no resource of type %q describes %q", errNoReference, ResourceTypeSBOM, targetIdentity)
	}

	sboms := make([]repository.SBOM, 0, len(describing))
	for _, candidate := range describing {
		identity := candidate.ToIdentity()

		data, err := req.Download(ctx, candidate, identity)
		if err != nil {
			return nil, fmt.Errorf("downloading sbom resource %q failed: %w", identity, err)
		}
		var buf bytes.Buffer
		if err := ocmblob.Copy(&buf, data); err != nil {
			return nil, fmt.Errorf("reading sbom resource %q failed: %w", identity, err)
		}

		req.Logger.Info("found an sbom resource referencing the requested resource",
			slog.String("sbom", identity.String()),
			slog.String("resource", targetIdentity.String()))

		sboms = append(sboms, repository.SBOM{
			Platform: repository.Platform{
				OS:           candidate.ExtraIdentity[attributeOS],
				Architecture: candidate.ExtraIdentity[attributeArchitecture],
				Variant:      candidate.ExtraIdentity[attributeVariant],
			},
			ID:            identity.String(),
			Name:          candidate.Name,
			PredicateType: detectPredicateType(buf.Bytes()),
			Data:          buf.Bytes(),
		})
	}

	return sboms, nil
}

// fromAttestations inspects the artifact the resource itself points at, for SBOMs
// attached to it at build time.
func fromAttestations(ctx context.Context, req Request) ([]repository.SBOM, error) {
	targetIdentity := req.Resource.ToIdentity()

	access := req.Resource.GetAccess()
	if access == nil {
		return nil, fmt.Errorf("no sbom found for resource %q: nothing in the component version references it, and it has no access to inspect for an attached one", targetIdentity)
	}

	notInspectable := func(reason error) error {
		return fmt.Errorf("no sbom found for resource %q: nothing in the component version references it, and its access type %q cannot be inspected for an attached sbom (%w)",
			targetIdentity, access.GetType(), reason)
	}

	plugin, err := req.PluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
	if err != nil {
		return nil, notInspectable(err)
	}

	discoverer, ok := plugin.(repository.SBOMDiscoverer)
	if !ok {
		return nil, notInspectable(fmt.Errorf("%T does not support sbom discovery", plugin))
	}

	var creds runtime.Typed
	credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, req.Resource)
	if err != nil {
		req.Logger.Debug("no credential consumer identity for resource, continuing unauthenticated",
			slog.String("resource", targetIdentity.String()),
			slog.String("error", err.Error()))
	} else if creds, err = req.Credentials.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return nil, fmt.Errorf("getting credentials for resource %q failed: %w", targetIdentity, err)
	}

	sboms, err := discoverer.DiscoverSBOM(ctx, req.Resource, creds, req.Options...)
	if err != nil {
		return nil, err
	}

	req.Logger.Info("found sboms attached to the artifact of the requested resource",
		slog.String("resource", targetIdentity.String()),
		slog.Int("discovered", len(sboms)))

	return sboms, nil
}

// detectPredicateType identifies a document by its own contents. We use this output
// for file naming purposes.
func detectPredicateType(data []byte) string {
	var probe struct {
		SPDXVersion string `json:"spdxVersion"`
		BOMFormat   string `json:"bomFormat"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	switch {
	case probe.SPDXVersion != "":
		return repository.PredicateTypeSPDX
	case probe.BOMFormat != "":
		return repository.PredicateTypeCycloneDX
	default:
		return ""
	}
}
