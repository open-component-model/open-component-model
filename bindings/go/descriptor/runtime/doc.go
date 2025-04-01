// Package runtime defines an internal runtime to work
// with component descriptors in all schema versions without
// restricting the code to the public API for future major changes.
//
// Overall this package makes it easier to work with embedded types as well
// due to its reliance on interfaces instead of Raw like the external versions.
//
// This package should be preferred whenever working with non-serialization
// relevant routines (such as working with accesses or components)
//
// This package SHOULD NOT be used for serialization and any kind of
// attempt at serialization will fail on purpose, be buggy and is not
// supported!
//
// Instead, use dedicated converters to the scheme versions available.
//
// This package is not guaranteed to be stable, but will always
// support conversion from and to all advertised component descriptor
// schemes.
//
// Thus, it is much easier to migrate from/to than if one would program
// against the scheme directly.
//
// The conversions offered in this package are guaranteed to be lossless unless
// otherwise specified, so it is usually safe to convert back and forth between
// scheme versions.
package runtime
