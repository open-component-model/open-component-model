// Package dir provides functionality for handling directory-based inputs in the Open Component Model (OCM) constructor.
//
// This package implements input methods for both resources and sources that are backed by directories
// from the local filesystem. It supports optional compression, optional possibility to include or exclude files based
// on naming patterns as well as support for symbolic links.
//
// Key Features:
//   - File-based resource and source input processing
//   - Optional gzip compression support
//   - Support for inclusion or exclusion based on file naming patterns
//   - Support for symbolic links with an option to include their content
//   - Integration with the OCM blob system for efficient data handling
//   - No credential requirements (files are accessed directly from the filesystem)
//
// Usage:
//
//	The package provides an InputMethod that can be used to process dir inputs for both
//	resources and sources. The method automatically handles file reading, symbolic links, explicit inclusion
//	and exclusion of files based on name patterns and optional compression based on the provided specification.
//
// Example:
//
//	result, err := (&dir.InputMethod{}).ProcessResource(ctx, resource, nil)
//
// The package can use the v1.Dir specification which includes:
//   - Path: The filesystem path to the input file
//   - MediaType: Optional explicit media type (auto-detected if not provided)
//   - Compress: Boolean flag to enable gzip compression
//   - PreserveDir: Boolean flag to include top-level directory
//   - FollowSymlinks: Boolean flag to follow symbolic links and include respective content
//   - ExcludeFiles: a string array of name patterns to explicitly exclude (overrides IncludeFiles)
//   - IncludeFiles: a string array of name patterns to explicitly include (only these files will be included)
package dir
