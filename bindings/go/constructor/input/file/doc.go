// Package file provides functionality for handling file-based inputs in the Open Component Model (OCM) constructor.
//
// This package implements input methods for both resources and sources that are backed by files
// from the local filesystem. It supports automatic media type detection, optional compression,
// and provides a unified interface for file handling within the OCM ecosystem.
//
// Key Features:
//   - File-based resource and source input processing
//   - Automatic MIME type detection using the mimetype library.
//     Supported media types are based on the mimetype library's detection capabilities,
//     which covers a wide range of common file formats.
//   - Optional gzip compression support
//   - Integration with the OCM blob system for efficient data handling
//   - No credential requirements (files are accessed directly from the filesystem)
//
// Usage:
//
//	The package provides an InputMethod that can be used to process file inputs for both
//	resources and sources. The method automatically handles file reading, media type
//	detection, and optional compression based on the provided specification.
//
// Example:
//
//	result, err := (&file.InputMethod{}).ProcessResource(ctx, resource, nil)
//
// The package can use the v1.File specification which includes:
//   - Path: The filesystem path to the input file
//   - MediaType: Optional explicit media type (auto-detected if not provided)
//   - Compress: Boolean flag to enable gzip compression
package file
