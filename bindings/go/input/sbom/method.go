// Package sbom implements the SBoM/v1 resource input method. At component
// construction time it discovers the Software Bill of Materials (SBOM) attached
// to a referenced resource's OCI image, merges the discovered SBOM(s) into a
// single CycloneDX document, and embeds it as a local blob. It also attaches the
// ocm.software/sbom back-link label pointing at the referenced resource so the
// baked SBOM can later be discovered via descriptor.FindSBOMResources.
package sbom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/sbom/spec/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

// MustAddToScheme registers the SBoM/v1 input spec (and its aliases) in scheme.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.SBOM{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
		runtime.NewVersionedType(v1.LegacyType, v1.Version),
		runtime.NewUnversionedType(v1.LegacyType),
	)
}

// ImageSBOMDiscoverer resolves the plugin for a resource's OCI access and returns
// the SBOM(s) attached to that image. The CLI wires an adapter over the
// resource-plugin registry; tests inject a fake. Credentials are resolved by the
// constructor (via GetResourceCredentialConsumerIdentity) and passed in.
type ImageSBOMDiscoverer interface {
	// ResolveCredentialConsumerIdentity returns the credential consumer identity
	// for the given resource's access, so the constructor can resolve credentials
	// before discovery. Returning an error is non-fatal: discovery proceeds
	// without credentials.
	ResolveCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error)
	// DiscoverImageSBOMs returns the SBOM(s) attached to the resource's OCI image.
	DiscoverImageSBOMs(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) ([]oci.SBOM, error)
}

// InputMethod implements the SBoM/v1 resource input method.
type InputMethod struct {
	// Discoverer runs on-image SBOM discovery against a resolved access.
	Discoverer ImageSBOMDiscoverer
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity
// from the resolved OCI access so the constructor can resolve credentials for the
// on-image discovery. It returns an error when no identity can be derived, which
// the constructor treats as "proceed without credentials".
func (i *InputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	if i.Discoverer == nil {
		return nil, fmt.Errorf("sbom input method has no image SBOM discoverer configured")
	}
	spec := v1.SBOM{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}
	if spec.Access == nil {
		return nil, fmt.Errorf("sbom input for resource %q has no resolved access", resource.Name)
	}
	synthetic := &descriptor.Resource{}
	synthetic.Access = spec.Access
	return i.Discoverer.ResolveCredentialConsumerIdentity(ctx, synthetic)
}

// ProcessResource discovers the SBOM(s) for the referenced resource, merges them
// into a single CycloneDX document, attaches the ocm.software/sbom back-link
// label to the resource, and returns the merged document as local blob data.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	if i.Discoverer == nil {
		return nil, fmt.Errorf("sbom input method has no image SBOM discoverer configured")
	}

	spec := v1.SBOM{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}
	if spec.Access == nil {
		return nil, fmt.Errorf("sbom input for resource %q has no resolved access; the referenced resource must exist and carry an OCI image access", resource.Name)
	}

	// Discover the SBOM(s) attached to the referenced image.
	synthetic := &descriptor.Resource{}
	synthetic.Access = spec.Access
	sboms, err := i.Discoverer.DiscoverImageSBOMs(ctx, synthetic, credentials)
	if err != nil {
		return nil, fmt.Errorf("discovering SBOM for resource %q failed: %w", resource.Name, err)
	}
	if len(sboms) == 0 {
		return nil, fmt.Errorf("no SBOM discovered for resource %q; nothing to embed", resource.Name)
	}

	// Select exactly one SBOM. A multi-arch image attaches one SBOM per platform;
	// the reference's architecture (extraIdentity) picks which one. The original
	// SBOM document is embedded as-is (its native format, e.g. SPDX) without
	// conversion or merging.
	selected, err := selectSBOM(sboms, spec.Resource.Architecture(), resource.Name)
	if err != nil {
		return nil, err
	}

	data, mediaType, err := readBlob(selected.Blob, selected.MediaType)
	if err != nil {
		return nil, fmt.Errorf("reading discovered SBOM for resource %q failed: %w", resource.Name, err)
	}

	// Attach the ocm.software/sbom back-link label pointing at the subject.
	if err := attachSBOMLabel(resource, spec.Resource.Identity()); err != nil {
		return nil, fmt.Errorf("attaching SBOM label to resource %q failed: %w", resource.Name, err)
	}

	sbomBlob := inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(mediaType))
	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: sbomBlob,
	}, nil
}

// selectSBOM picks a single SBOM from the discovered set. When arch is set it
// matches the SBOM whose image architecture equals it (a bare "arch", or a full
// "os/arch[/variant]"). When arch is empty and exactly one SBOM was discovered,
// that one is used; otherwise the reference must set extraIdentity.architecture.
func selectSBOM(sboms []oci.SBOM, arch, resourceName string) (*oci.SBOM, error) {
	if arch == "" {
		if len(sboms) == 1 {
			return &sboms[0], nil
		}
		return nil, fmt.Errorf("resource %q image is multi-arch (%s); set resource.extraIdentity.architecture to select one", resourceName, strings.Join(availablePlatforms(sboms), ", "))
	}

	for idx := range sboms {
		if platformString(sboms[idx].Platform) == arch || archOf(sboms[idx].Platform) == arch {
			return &sboms[idx], nil
		}
	}
	return nil, fmt.Errorf("no SBOM for architecture %q on resource %q; available: %s", arch, resourceName, strings.Join(availablePlatforms(sboms), ", "))
}

// availablePlatforms lists the platform labels of the discovered SBOMs for use in
// error messages.
func availablePlatforms(sboms []oci.SBOM) []string {
	out := make([]string, 0, len(sboms))
	for idx := range sboms {
		if p := platformString(sboms[idx].Platform); p != "" {
			out = append(out, p)
		} else {
			out = append(out, "<unknown>")
		}
	}
	return out
}

// platformString renders a platform as "os/arch" or "os/arch/variant".
func platformString(p *ociImageSpecV1.Platform) string {
	if p == nil {
		return ""
	}
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}

func archOf(p *ociImageSpecV1.Platform) string {
	if p == nil {
		return ""
	}
	return p.Architecture
}

// readBlob reads a blob fully and returns its bytes and media type (falling back
// to the provided mediaType when the blob does not advertise one).
func readBlob(b blob.ReadOnlyBlob, mediaType string) (_ []byte, _ string, err error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, "", err
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", err
	}
	if mta, ok := b.(blob.MediaTypeAware); ok {
		if mt, known := mta.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}
	return data, mediaType, nil
}

// attachSBOMLabel appends (or replaces) the ocm.software/sbom label on the
// resource, linking the baked SBOM to the subject resource identity.
func attachSBOMLabel(resource *constructorruntime.Resource, subject runtime.Identity) error {
	value := descriptor.SBOMLabelValue{
		References: []descriptor.SBOMReference{{Resource: subject}},
	}
	label := constructorruntime.Label{
		Name:    descriptor.LabelSBOM,
		Version: "v1",
		Signing: true,
	}
	// Marshal to JSON explicitly: SetValue's struct path runs values through a
	// YAML encoder, which would store YAML text in the JSON-typed label value and
	// break later descriptor JSON marshalling. Passing JSON bytes keeps it JSON.
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := label.SetValue(valueJSON); err != nil {
		return err
	}
	// Replace any pre-existing label of the same name to stay idempotent.
	for idx := range resource.Labels {
		if resource.Labels[idx].Name == descriptor.LabelSBOM {
			resource.Labels[idx] = label
			return nil
		}
	}
	resource.Labels = append(resource.Labels, label)
	return nil
}
