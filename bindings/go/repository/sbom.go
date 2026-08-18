package repository

import (
	"cmp"
	"context"
	goruntime "runtime"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// PredicateTypeSPDX is the predicate type of an SPDX document.
	PredicateTypeSPDX = "https://spdx.dev/Document"
	// PredicateTypeCycloneDX is the predicate type of a CycloneDX document.
	PredicateTypeCycloneDX = "https://cyclonedx.org/bom"

	// MediaTypeSPDXJSON media type of an SPDX document.
	MediaTypeSPDXJSON = "application/spdx+json"
	// MediaTypeCycloneDXJSON media type of a CycloneDX document.
	MediaTypeCycloneDXJSON = "application/vnd.cyclonedx+json"
)

// SBOMDiscoverer is an optional capability of a ResourceRepository that can retrieve
// the SBOMs describing a resource. How those SBOMs are attached to the resource is up
// to the implementation.
type SBOMDiscoverer interface {
	// DiscoverSBOM returns every SBOM describing the resource that satisfies opts,
	// authenticating against the repository with the given credentials.
	DiscoverSBOM(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, opts ...SBOMOption) ([]SBOM, error)
}

// SBOM is one SBOM document discovered for a resource.
type SBOM struct {
	// Platform of the artifact this SBOM describes. Zero when the source carries none.
	Platform Platform
	// ID identifies the document within the resource. It is the only field guaranteed
	// to be unique across the SBOMs of one resource.
	ID string
	// Name is the document's own name. Empty for producers that do not name their
	// documents, so it is not a reliable identifier on its own.
	Name string
	// PredicateType names the kind of document Data holds, for example
	// PredicateTypeSPDX. Empty when it could not be determined.
	PredicateType string
	// Data is the SBOM document itself.
	Data []byte
}

// String names the document without its contents, so that formatting an SBOM with
// %v or %q cannot spill the whole document into a log line or an error message.
func (s SBOM) String() string {
	name := cmp.Or(s.Name, s.ID, "unnamed sbom")
	if platform := s.Platform.String(); platform != "" {
		return name + " (" + platform + ")"
	}
	return name
}

// MediaType returns the media type matching PredicateType.
func (s SBOM) MediaType() string {
	switch s.PredicateType {
	case PredicateTypeSPDX:
		return MediaTypeSPDXJSON
	case PredicateTypeCycloneDX:
		return MediaTypeCycloneDXJSON
	default:
		return "application/json"
	}
}

// Platform is the build target of an artifact. It mirrors the OCI platform attributes
// without depending on the image spec.
type Platform struct {
	OS           string
	Architecture string
	Variant      string
	OSVersion    string
	OSFeatures   []string
}

// String renders the platform the conventional way, "os/architecture/variant",
// leaving out the attributes that are not set. Empty for the zero platform.
func (p Platform) String() string {
	set := make([]string, 0, 3)
	for _, attribute := range []string{p.OS, p.Architecture, p.Variant} {
		if attribute != "" {
			set = append(set, attribute)
		}
	}
	return strings.Join(set, "/")
}

// SBOMOptions configures SBOM discovery.
type SBOMOptions struct {
	// Platform narrows the discovery to one platform. Ignored when AllPlatforms is set.
	Platform Platform
	// AllPlatforms widens the discovery to every platform the resource offers.
	AllPlatforms bool
	// PredicateTypes narrows the discovery to documents of these types.
	PredicateTypes []string
}

// SBOMOption configures SBOM discovery.
type SBOMOption func(*SBOMOptions)

// WithAllSBOMPlatforms widens the discovery to every platform the resource offers,
// ignoring any platform set by WithSBOMPlatform.
func WithAllSBOMPlatforms() SBOMOption {
	return func(o *SBOMOptions) {
		o.AllPlatforms = true
	}
}

// WithSBOMPlatform narrows the discovery to a platform. It refines attribute by
// attribute, so a caller can override a single attribute of an already set platform.
func WithSBOMPlatform(platform Platform) SBOMOption {
	return func(o *SBOMOptions) {
		if platform.Architecture != "" {
			o.Platform.Architecture = platform.Architecture
		}
		if platform.OS != "" {
			o.Platform.OS = platform.OS
		}
		if platform.Variant != "" {
			o.Platform.Variant = platform.Variant
		}
		if platform.OSVersion != "" {
			o.Platform.OSVersion = platform.OSVersion
		}
		if len(platform.OSFeatures) > 0 {
			o.Platform.OSFeatures = platform.OSFeatures
		}
	}
}

// WithSBOMPredicateTypes sets the predicate types to discover.
// Default is PredicateTypeSPDX.
func WithSBOMPredicateTypes(types ...string) SBOMOption {
	return func(o *SBOMOptions) {
		if len(types) == 0 {
			return
		}
		o.PredicateTypes = types
	}
}

// NewSBOMOptions applies opts over the discovery defaults. Implementations of
// SBOMDiscoverer use it to resolve the options they were handed.
func NewSBOMOptions(opts ...SBOMOption) *SBOMOptions {
	o := &SBOMOptions{
		// Do not default OS otherwise, we'll restrict artifacts not on the current os.
		Platform: Platform{
			Architecture: goruntime.GOARCH,
		},
		PredicateTypes: []string{PredicateTypeSPDX},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
