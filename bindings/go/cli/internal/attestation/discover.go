// Package attestation discovers the attestations describing a resource or a whole
// component and merges them across every source. Discovery is metadata-only: it records
// what is attached (source and raw type signals) without downloading documents or
// interpreting them.
package attestation

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artifactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artifactref/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// defaultConcurrency bounds how many resources are discovered at once when none is
// requested. Discovery is latency-bound (one registry round-trip per referrer), so a
// small fan-out already collapses the wall time without hammering the registry.
const defaultConcurrency = 4

// Group is the attestations discovered for one origin: a resource (Label is its identity)
// or the component version artifact itself (Label is empty).
type Group struct {
	Label        string
	Attestations []repository.Attestation
	// Reference is the artifact location of a resource origin (the access
	// imageReference), shown when its discovery fails so the failing image is
	// named rather than only its ocm identity. Empty for the component-level
	// group and for accesses that expose no image reference.
	Reference string
	// Err is set when discovery for this origin failed. The group is then kept with no
	// attestations so the origin (a resource, or the component) stays visible and the
	// failure is reported in the output rather than only in a log line.
	Err error
}

// DiscoverAll lists attestations across the whole component: a component-level group
// (referrers on the component version artifact) plus one group per resource. Resources
// are discovered concurrently, bounded by concurrency (<=0 applies defaultConcurrency).
// Best-effort: a resource whose discovery fails is logged and skipped, never fatal and
// never cancelling its siblings. Groups keep descriptor order, component first; a group
// with no attestations and no error is dropped, but a failed group is kept carrying its
// error so the origin stays visible. It errors only when nothing was discovered anywhere
// and a source failed, so an empty success cannot mask a wholesale failure.
func DiscoverAll(ctx context.Context, req Request, concurrency int) ([]Group, error) {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	resources := req.Descriptor.Component.Resources
	// Slot 0 is the component-level group; slot i+1 is resource i. Each goroutine writes
	// its own group slot and its own error slot, so no lock is needed.
	slots := make([]Group, len(resources)+1)
	errs := make([]error, len(resources)+1)

	componentReq := req
	componentReq.Resource = nil
	if atts, err := Discover(ctx, componentReq); err != nil {
		req.Logger.Warn("discovering component-level attestations failed",
			slog.String("component", req.Descriptor.Component.Name),
			slog.String("version", req.Descriptor.Component.Version),
			slog.Any("error", err))
		errs[0] = err
		slots[0] = Group{Err: err}
	} else {
		slots[0] = Group{Attestations: atts}
	}

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrency)
	for i := range resources {
		eg.Go(func() error {
			resReq := req
			resReq.Resource = &req.Descriptor.Component.Resources[i]
			reference := accessReference(resReq.Resource)
			atts, err := Discover(gctx, resReq)
			if err != nil {
				req.Logger.Warn("discovering resource attestations failed",
					slog.String("resource", resReq.Resource.ToIdentity().String()),
					slog.Any("error", err))
				errs[i+1] = err
				slots[i+1] = Group{Label: resReq.Resource.ToIdentity().String(), Reference: reference, Err: err}
				return nil // best-effort: a failed resource never cancels the others
			}
			slots[i+1] = Group{Label: resReq.Resource.ToIdentity().String(), Reference: reference, Attestations: atts}
			return nil
		})
	}
	_ = eg.Wait() // workers never return an error; Wait only joins them

	out := make([]Group, 0, len(slots))
	anyAttestations := false
	for _, g := range slots {
		if len(g.Attestations) > 0 {
			anyAttestations = true
		}
		if len(g.Attestations) > 0 || g.Err != nil {
			out = append(out, g)
		}
	}
	// Partial failures are kept per group and reported there. Only when nothing was
	// discovered anywhere and a source failed is it a wholesale failure (bad credentials,
	// registry down): surface the joined error so the exit code reflects it.
	if !anyAttestations {
		if err := errors.Join(errs...); err != nil {
			return nil, fmt.Errorf("discovering attestations failed for every source: %w", err)
		}
	}
	return out, nil
}

const (
	attributeOS           = "os"
	attributeArchitecture = "architecture"
	attributeVariant      = "variant"
)

// Request has everything Discover needs to list the attestations of one resource, or of
// the whole component when Resource is nil.
type Request struct {
	Descriptor *descriptor.Descriptor
	// Resource is the resource whose attestations to list. Nil selects component level:
	// the referrers attached to the component version artifact itself.
	Resource      *descriptor.Resource
	PluginManager *manager.PluginManager
	Credentials   credentials.Resolver
	// DiscoverLocal lists attestations of artifacts copied into the component as local
	// blobs (by-value transfer). Optional; nil disables that path.
	DiscoverLocal repository.LocalAttestationDiscoverer
	// DiscoverComponent lists referrers on the component version artifact. Used only in
	// component-level mode (Resource nil); nil disables it.
	DiscoverComponent repository.ComponentAttestationDiscoverer
	Logger            *slog.Logger
}

// errAttachedDiscovery marks a failure to list the attestations attached in the registry
// (OCI referrers, BuildKit in-toto). Discover demotes it to a warning when the resource
// still has locally declared attestations; setup failures (plugin lookup, credential
// resolution) are not wrapped with it and always propagate.
var errAttachedDiscovery = errors.New("discovering attached attestations failed")

// Discover lists every attestation describing the requested resource, merged across
// sources and deduplicated. With Resource nil it lists the referrers on the component
// version artifact instead. A failure to reach the registry does not discard the
// attestations declared locally in the component.
func Discover(ctx context.Context, req Request) ([]repository.Attestation, error) {
	if req.Resource == nil {
		if req.DiscoverComponent == nil {
			return nil, nil
		}
		atts, err := req.DiscoverComponent.DiscoverComponentAttestations(ctx, req.Descriptor.Component.Name, req.Descriptor.Component.Version)
		if err != nil {
			return nil, err
		}
		return dedup(atts), nil
	}

	all, err := fromArtifactReferences(req)
	if err != nil {
		return nil, err
	}

	attached, err := fromDiscoverer(ctx, req)
	if err != nil {
		if len(all) == 0 || !errors.Is(err, errAttachedDiscovery) {
			return nil, err
		}
		req.Logger.Warn("discovering attached attestations failed, returning locally declared ones only",
			slog.String("resource", req.Resource.ToIdentity().String()),
			slog.Any("error", err))
	} else {
		all = append(all, attached...)
	}

	return dedup(all), nil
}

// fromArtifactReferences lists the resources of the component that declare, through the
// artifact reference label, that they describe the target. Metadata-only: it reads each
// resource's declared type and blob media type without downloading the blob.
func fromArtifactReferences(req Request) ([]repository.Attestation, error) {
	target := req.Resource.ToIdentity()

	referring, err := artifactref.FindDescribingResources(req.Descriptor, target)
	if err != nil {
		if errors.Is(err, artifactref.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	out := make([]repository.Attestation, 0, len(referring))
	for _, candidate := range referring {
		out = append(out, repository.Attestation{
			Source:       repository.AttestationSourceOCMLocal,
			ArtifactType: candidate.Type,
			MediaType:    accessMediaType(candidate),
			Platform: repository.Platform{
				OS:           candidate.ExtraIdentity[attributeOS],
				Architecture: candidate.ExtraIdentity[attributeArchitecture],
				Variant:      candidate.ExtraIdentity[attributeVariant],
			},
			ID: candidate.ToIdentity().String(),
			// The local blob reference is the content digest of the document itself, so a
			// local attestation shares it with a referrer carrying the same bytes.
			ContentDigest: accessLocalReference(candidate),
		})
	}
	return out, nil
}

// fromDiscoverer asks the resource plugin backing the target's access to list the
// attestations attached to the artifact (OCI referrers and BuildKit in-toto). Returns
// nil without error when the access carries none or the plugin does not support it.
func fromDiscoverer(ctx context.Context, req Request) ([]repository.Attestation, error) {
	access := req.Resource.GetAccess()
	if access == nil {
		return nil, nil
	}

	if v2.IsLocalBlob(access) {
		if req.DiscoverLocal == nil {
			return nil, nil
		}
		return req.DiscoverLocal.DiscoverLocalAttestations(ctx, req.Descriptor.Component.Name, req.Descriptor.Component.Version, req.Resource.ToIdentity())
	}

	plugin, err := req.PluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
	if err != nil {
		return nil, fmt.Errorf("getting resource plugin for access %q failed: %w", access.GetType(), err)
	}
	discoverer, ok := plugin.(repository.AttestationDiscoverer)
	if !ok {
		req.Logger.Debug("resource plugin does not support attestation discovery",
			slog.String("resource", req.Resource.ToIdentity().String()),
			slog.String("access", access.GetType().String()))
		return nil, nil
	}

	var creds runtime.Typed
	credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, req.Resource)
	if err != nil {
		req.Logger.Debug("no credential consumer identity for resource, continuing unauthenticated",
			slog.String("resource", req.Resource.ToIdentity().String()),
			slog.String("error", err.Error()))
	} else if creds, err = req.Credentials.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return nil, fmt.Errorf("getting credentials for resource %q failed: %w", req.Resource.ToIdentity(), err)
	}

	atts, err := discoverer.DiscoverAttestations(ctx, req.Resource, creds)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAttachedDiscovery, err)
	}
	return atts, nil
}

// dedup removes attestations discovered through more than one source, keyed by the
// subject digest and the attestation's own ID, keeping the first occurrence.
func dedup(in []repository.Attestation) []repository.Attestation {
	seen := make(map[string]struct{}, len(in))
	out := make([]repository.Attestation, 0, len(in))
	for _, a := range in {
		key := a.Digest + "\x00" + a.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, a)
	}
	return out
}

// accessMediaType reads the declared media type of a resource's access (for a local
// blob) from its metadata, without downloading the blob. Empty when none is declared.
func accessMediaType(res *descriptor.Resource) string {
	access := res.GetAccess()
	if access == nil {
		return ""
	}
	data, err := json.Marshal(access, runtime.JSONOptions)
	if err != nil {
		return ""
	}
	var probe struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return probe.MediaType
}

// accessLocalReference reads the local blob reference (the document content digest) of a
// resource's access from its metadata, without downloading the blob. Empty when none is
// declared (for example a non-local access).
func accessLocalReference(res *descriptor.Resource) string {
	access := res.GetAccess()
	if access == nil {
		return ""
	}
	data, err := json.Marshal(access, runtime.JSONOptions)
	if err != nil {
		return ""
	}
	var probe struct {
		LocalReference string `json:"localReference"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return probe.LocalReference
}

// accessReference reads the artifact location of a resource's access (the OCI
// imageReference) from its metadata, without downloading the blob. Empty when the
// access declares none (for example a local blob). It names the failing image when a
// resource's attestation discovery fails.
func accessReference(res *descriptor.Resource) string {
	access := res.GetAccess()
	if access == nil {
		return ""
	}
	data, err := json.Marshal(access, runtime.JSONOptions)
	if err != nil {
		return ""
	}
	var probe struct {
		ImageReference string `json:"imageReference"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return probe.ImageReference
}
