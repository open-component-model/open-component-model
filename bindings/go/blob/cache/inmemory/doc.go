// Package inmemory provides an in-memory caching implementation for blob storage.
//
// The package implements a caching layer for ReadOnlyBlob that stores blob data in memory
// after the first read, enabling efficient repeated access to the same data. This is
// particularly useful when:
//   - The same blob needs to be accessed multiple times
//   - The underlying blob source is expensive to read from
//   - The blob data is relatively small and can be kept in memory
//
// The implementation is thread-safe and handles both known and unknown blob sizes.
// When the size is known, it uses exact buffer sizes for optimal memory usage.
// For unknown sizes, it falls back to dynamic buffer allocation.
//
// The cache can be cleared using the ClearCache method to free up memory when the
// cached data is no longer needed.
//
// Note that this is mainly intended for use cases with repeated I/O operations on the same blob.
// For one-time reads, it may be more efficient to read directly from the underlying blob source.
// Similarly, when only working with simple Readers, one might want to consider a EagerBufferedReader
// implementation instead.
//
// Example usage:
//
//	blob := inmemory.Cache(sourceBlob)
//	data, err := blob.Data()  // First read caches the data
//	data, err = blob.Data()   // Subsequent reads use cached data
//	blob.ClearCache()         // Clear cache when done
package inmemory
