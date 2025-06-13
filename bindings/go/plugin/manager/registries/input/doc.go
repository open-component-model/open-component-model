// Package input provides a registry for managing input method plugins.
//
// This package implements a plugin registry system that manages both internal and external
// input method plugins. It handles the registration, lifecycle management, and execution
// of plugins that provide input methods for resources and sources in the Open Component Model.
//
// The caller of the plugin manager deals with exact interfaces sitting in their respective
// packages, while the external plugins are using serializable interface contracts. The conversion
// between the two happens in resource_input_converter.go and source_input_converter.go.
package input
