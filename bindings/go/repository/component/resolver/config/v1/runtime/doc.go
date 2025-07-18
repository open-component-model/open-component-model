// Package runtime defines an internal runtime to work
// with resolver configurations.
//
// Overall, this package makes it easier to work with embedded types due to its
// reliance on interfaces instead of Raw like the external versions.
//
// This package should be preferred whenever working with non-serialization
// relevant routines (such as working with accesses or components).
//
// This package SHOULD NOT be used for serialization, and any kind of
// attempt at serialization will fail on purpose, be buggy and is not
// supported!
//
// Instead, use dedicated converters to the scheme versions available.
//
// The conversions offered in this package are guaranteed to be lossless unless
// otherwise specified, so it is usually safe to convert back and forth between
// scheme versions.
package runtime
