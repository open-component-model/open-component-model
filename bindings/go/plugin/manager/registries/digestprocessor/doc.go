// Package digestprocessor provides a registry for managing digest processor plugins.
//
// This package implements a plugin registry system that manages both internal and external
// digest processor plugins. It handles the registration, lifecycle management, and
// execution of plugins that process resource digests in the Open Component Model.
//
// The caller of the plugin manager deals with exact interfaces sitting in their respective
// packages, while the external plugins are using serializable interface contracts. The conversion
// between the two happens in resource_digest_processor_converter.go.
package digestprocessor
