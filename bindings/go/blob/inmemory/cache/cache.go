package cache

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/blob"
	inmemory "ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// Cache creates a new in-memory, eagerly cached blob from a ReadOnlyBlob.
// The returned blob will hold the data of the original blob in memory when this function is called.
// If the caching fails, it returns an error.
func Cache(b blob.ReadOnlyBlob) (_ *inmemory.Blob, err error) {
	var options []inmemory.MemoryBlobOption
	// 1. Attempt to retrieve size from blob
	size := blob.SizeUnknown
	if sizeAware, ok := b.(blob.SizeAware); ok {
		size = sizeAware.Size()
	}
	if size != blob.SizeUnknown {
		options = append(options, inmemory.WithSize(size))
	}

	// 2. Attempt to retrieve digest from blob
	var digest string
	if digestAware, ok := b.(blob.DigestAware); ok {
		if d, ok := digestAware.Digest(); ok {
			digest = d
		}
	}
	if digest != "" {
		options = append(options, inmemory.WithDigest(digest))
	}

	// 3. Attempt to retrieve media type from blob
	var mediaType string
	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		if mediaTypeFromBlob, ok := mediaTypeAware.MediaType(); ok {
			mediaType = mediaTypeFromBlob
		}
	}
	if mediaType != "" {
		options = append(options, inmemory.WithMediaType(mediaType))
	}

	// Get a reader from the source
	readCloser, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, readCloser.Close())
	}()

	cached := inmemory.New(readCloser, options...)

	// this triggers the eager loading of the blob data into memory,
	// after that we can safely close the original readCloser.
	if err := cached.Load(); err != nil {
		return nil, err
	}

	return cached, nil
}
