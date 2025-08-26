// Package cache provides an in-memory caching implementation for blob storage.
//
// The package implements a caching layer for ReadOnlyBlob that stores blob data in memory
// after the first read, enabling efficient repeated access to the same data. This is
// particularly useful when:
//   - The same blob needs to be accessed multiple times
//   - The underlying blob source is expensive to read from
//   - The blob data is relatively small and can be kept in memory
//   - The size and digest of the blob are not known and need to be computed on the first read
//
// The implementation is thread-safe and handles both known and unknown blob sizes.
// When the size is known, it uses exact buffer sizes for optimal memory usage.
// For unknown sizes, it falls back to dynamic buffer allocation.
//
// Example usage:
//
//	blob, err := inmemory.Cache(sourceBlob) // if the loading into the cache fails, an error is returned
//	data := blob.Data()  // The data can be directly accessed after caching.
//	data, _ := blob.ReadCloser() // or accessed via ReadCloser for incremental reading
package cache
