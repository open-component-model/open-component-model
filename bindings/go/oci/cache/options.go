package cache

import (
	"errors"
	"fmt"
	"strings"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	"ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// RemotePolicy controls when the cache contacts the upstream registry
// on a cache hit.
type RemotePolicy string

const (
	// RemotePolicyIfNotPresent returns cached content without contacting
	// the registry. The local cache directory is trusted.
	//
	// This governs authorisation, not freshness: tag resolution always
	// reaches the registry because a tag is mutable and no local signal
	// reports that it moved. Only digest references — which are
	// content-addressed and therefore immutable — are served from the
	// [ReferenceCache] without a round-trip.
	RemotePolicyIfNotPresent RemotePolicy = "IfNotPresent"

	// RemotePolicyAlways contacts the registry on every cache hit to
	// verify the caller is authorised. Mirrors Kubernetes imagePullPolicy:Always.
	RemotePolicyAlways RemotePolicy = "Always"
)

// Options controls the behaviour of both the [BlobCache] and the
// [ReferenceCache]. The two caches share the same Options struct so a
// caller can configure them once and instantiate either or both with
// matching limits.
//
// Dir is required at construction (see [Options.applyDefaults]) even
// though it lives on Options rather than the constructor signature:
// keeping every parameter on a single struct lets callers configure
// both caches from one shared config source without a two-argument
// constructor. [Defaults] returns an Options populated with sensible
// limits; the per-field defaults are also applied automatically by
// [NewBlobCache] / [NewReferenceCache] when a caller passes a
// partially-populated Options.
//
// MaxBlobSize and Accept are blob-specific and ignored by
// [ReferenceCache].
type Options struct {
	// Dir is the absolute directory the cache owns — required. Both
	// caches lay out their files inside Dir without colliding
	// ([BlobCache] uses `<Dir>/blobs/<algo>/<hex>` files,
	// [ReferenceCache] uses `<Dir>/refs/<sha256(namespace)>.json`), so
	// the same Dir can be shared.
	Dir string

	// MaxEntries bounds the LRU. A value of 0 means unlimited
	// (subject only to TTL).
	MaxEntries int

	// TTL is the maximum age of a cache entry. A value of 0 disables
	// time-based expiry; entries then live until LRU overflow.
	TTL time.Duration

	// MaxBlobSize is the per-blob size cap for the [BlobCache].
	// Descriptors with a larger Size are fetched but not cached.
	// A value of 0 disables the cap. Ignored by [ReferenceCache].
	MaxBlobSize int64

	// Accept reports whether a blob should be cached by the
	// [BlobCache]. The full descriptor is passed (not just the media
	// type) so callers can filter on annotations, artifact type, or
	// size without a breaking signature change. nil falls back to
	// [DefaultAccept] in [NewBlobCache]. Ignored by [ReferenceCache].
	Accept func(desc ociImageSpecV1.Descriptor) bool

	// RemotePolicy controls whether the upstream registry is contacted
	// on a cache hit. Defaults to [RemotePolicyIfNotPresent].
	RemotePolicy RemotePolicy
}

// Defaults returns an [Options] populated with sane caching limits:
//   - 256 entries
//   - 10 minute TTL
//   - 4 MiB per-blob size cap
//   - [DefaultAccept] media-type filter
//   - [RemotePolicyAlways] (secure by default)
//
// Dir is left zero — callers must set it explicitly.
func Defaults() *Options {
	return &Options{
		MaxEntries:   256,
		TTL:          10 * time.Minute,
		MaxBlobSize:  4 << 20,
		Accept:       DefaultAccept,
		RemotePolicy: RemotePolicyAlways,
	}
}

// applyDefaults validates required fields and fills zero-valued
// fields from [Defaults].
func (o Options) applyDefaults() (Options, error) {
	if o.Dir == "" {
		return Options{}, errors.New("cache: Options.Dir is required")
	}
	d := Defaults()
	if o.MaxEntries == 0 {
		o.MaxEntries = d.MaxEntries
	}
	if o.TTL == 0 {
		o.TTL = d.TTL
	}
	if o.MaxBlobSize == 0 {
		o.MaxBlobSize = d.MaxBlobSize
	}
	if o.Accept == nil {
		o.Accept = d.Accept
	}
	switch o.RemotePolicy {
	case "":
		o.RemotePolicy = d.RemotePolicy
	case RemotePolicyIfNotPresent, RemotePolicyAlways:
		// ok
	default:
		return Options{}, fmt.Errorf("cache: unsupported RemotePolicy %q", o.RemotePolicy)
	}
	return o, nil
}

// DefaultAccept is the default admission filter. It accepts:
//   - any OCI/Docker manifest or index media type
//     (see [introspection.IsOCICompliantMediaType]);
//   - the OCM component config media type;
//   - any OCM component-descriptor media type — i.e. anything with the
//     prefix [descriptor.MediaTypeComponentDescriptor], which covers
//     v2+json, v2+yaml, legacy v1 variants, and the legacy +tar
//     wrapper.
//
// The size filter is applied separately via [Options.MaxBlobSize] so
// custom [Options.Accept] implementations do not have to re-encode it.
func DefaultAccept(desc ociImageSpecV1.Descriptor) bool {
	mediaType := desc.MediaType
	if introspection.IsOCICompliantMediaType(mediaType) {
		return true
	}
	if mediaType == componentConfig.MediaType {
		return true
	}
	return strings.HasPrefix(mediaType, descriptor.MediaTypeComponentDescriptor)
}
