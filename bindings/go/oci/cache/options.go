package cache

import (
	"errors"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	"ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// Options controls the behaviour of both the [BlobCache] and the
// [ReferenceCache]. The two caches share the same Options struct so a
// caller can configure them once and instantiate either or both with
// matching limits.
//
// At minimum [Options.Dir] must be set. [Defaults] returns an Options
// populated with sensible limits; the per-field defaults are also
// applied automatically by [NewBlobCache] / [NewReferenceCache] when
// a caller passes a partially-populated Options.
//
// MaxBlobSize and Accept are blob-specific and ignored by
// [ReferenceCache].
type Options struct {
	// Dir is the absolute directory the cache owns. Both caches lay
	// out their files inside Dir without colliding ([BlobCache] uses
	// `<Dir>/<algo>/<hex>` files, [ReferenceCache] uses
	// `<Dir>/references.json`), so the same Dir can be shared.
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

	// Accept reports whether a media type should be cached by the
	// [BlobCache]. nil falls back to [DefaultAccept] in [NewBlobCache].
	// Ignored by [ReferenceCache].
	Accept func(mediaType string) bool
}

// Defaults returns an [Options] populated with sane caching limits:
//   - 256 entries
//   - 10 minute TTL
//   - 4 MiB per-blob size cap
//   - [DefaultAccept] media-type filter
//
// Dir is left zero — callers must set it explicitly.
func Defaults() *Options {
	return &Options{
		MaxEntries:  256,
		TTL:         10 * time.Minute,
		MaxBlobSize: 4 << 20,
		Accept:      DefaultAccept,
	}
}

// applyDefaults validates required fields and fills zero-valued
// fields from [Defaults].
func (o Options) applyDefaults() (Options, error) {
	if o.Dir == "" {
		return Options{}, errors.New("blobcache: Options.Dir is required")
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
	return o, nil
}

// DefaultAccept is the default media-type filter. It accepts:
//   - any OCI/Docker manifest or index media type
//     (see [introspection.IsOCICompliantMediaType]);
//   - the OCM component config media type;
//   - any OCM component-descriptor media type — i.e. anything with the
//     prefix [descriptor.MediaTypeComponentDescriptor], which covers
//     v2+json, v2+yaml, legacy v1 variants, and the legacy +tar
//     wrapper.
func DefaultAccept(mediaType string) bool {
	if introspection.IsOCICompliantMediaType(mediaType) {
		return true
	}
	if mediaType == componentConfig.MediaType {
		return true
	}
	return strings.HasPrefix(mediaType, descriptor.MediaTypeComponentDescriptor)
}
