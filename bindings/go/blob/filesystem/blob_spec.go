package filesystem

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/spec/access"
	"ocm.software/open-component-model/bindings/go/blob/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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
