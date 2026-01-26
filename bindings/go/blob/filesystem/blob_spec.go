package filesystem

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetBlobFromSpec converts a typed access specification to a ReadOnlyBlob.
//
// The function supports File access specifications (v1alpha1.File) with "file://" URI schemes.
// It extracts the file path from the URI, resolves it to an absolute path, and creates
// a blob using GetBlobFromPath with the media type from the spec (if provided).
//
// The returned blob is created with reproducible options enabled, meaning TAR archives
// will have normalized timestamps and ownership information for consistent digests.
//
// Returns an error if:
//   - The spec cannot be converted to a File type
//   - The URI is invalid or has an unsupported scheme
//   - The file path is empty
//   - The absolute path cannot be resolved
//   - The blob cannot be created from the path
func GetBlobFromSpec(ctx context.Context, spec runtime.Typed) (blob.ReadOnlyBlob, error) {
	var file v1alpha1.File
	if err := access.Scheme.Convert(spec, &file); err != nil {
		return nil, fmt.Errorf("cannot convert spec to File: %w", err)
	}

	parsed, err := url.Parse(file.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid URI: %w", err)
	}

	switch parsed.Scheme {
	case "file":
		path := parsed.Path
		if path == "" {
			return nil, fmt.Errorf("empty file path in URI")
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve absolute path: %w", err)
		}
		return GetBlobFromPath(ctx, absPath, DirOptions{
			MediaType:    file.MediaType,
			Reproducible: true,
		})
	default:
		return nil, fmt.Errorf("unsupported URI scheme: %s", parsed.Scheme)
	}
}

// BlobToSpec writes a blob to the filesystem and returns a File access specification.
//
// The function copies the blob content to the specified path using CopyBlobToOSPath,
// then creates a v1alpha1.File specification with a "file://" URI pointing to that path.
//
// If the blob implements blob.MediaTypeAware, the media type is extracted and included
// in the spec. If the blob implements blob.DigestAware, the digest is extracted and
// included in the spec.
//
// The returned File spec has its type field populated with the default type from the
// access scheme registry.
//
// Returns an error if:
//   - The blob content cannot be copied to the path
//
// Note: The path should be an absolute path to avoid ambiguity in the resulting URI.
func BlobToSpec(content blob.ReadOnlyBlob, path string) (*v1alpha1.File, error) {
	err := CopyBlobToOSPath(content, path)
	if err != nil {
		return nil, err
	}

	var mediaType string
	if mediaTypeAware, ok := content.(blob.MediaTypeAware); ok {
		mediaType, _ = mediaTypeAware.MediaType()
	}
	var digest string
	if digestAware, ok := content.(blob.DigestAware); ok {
		digest, _ = digestAware.Digest()
	}

	spec := &v1alpha1.File{
		URI:       "file://" + path,
		MediaType: mediaType,
		Digest:    digest,
	}
	_, _ = access.Scheme.DefaultType(spec)

	return spec, nil
}
