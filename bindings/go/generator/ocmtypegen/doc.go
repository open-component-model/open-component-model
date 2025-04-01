// Package main provides a code generation utility for Go struct types
// that are marked with a special comment and include a field of type
// `runtime.Type` from the OCM Go bindings.
//
// This generator scans all Go source files within a specified root
// directory, looking for struct type declarations that:
//  1. Are annotated with the marker comment `+ocm:typegen=true`
//  2. Contain a field named `Type` of type `runtime.Type`
//
// For each matching struct, the generator produces a Go source file
// (`zz_generated.ocm_type.go`) in the same package directory. The generated
// file includes two methods for each struct:
//
//   - `SetType(runtime.Type)`
//   - `GetType() runtime.Type`
//
// These methods assist in runtime type inference, defaulting, and adherence
// to OCM framework interfaces.
//
// Usage:
//
//	go run main.go <root-directory>
//
// Example:
//
//	go run main.go ./internal/components
//
// This will recursively search all subfolders under `./internal/components`,
// locate annotated structs with the correct field, and generate the
// appropriate code in each package.
//
// Generated File:
//
//	The output file is named `zz_generated.ocm_type.go` and contains a build
//	constraint to prevent accidental editing. It is safe to regenerate at any time.
//
// Limitations:
//   - The tool only processes `.go` source files and skips `_test.go` files.
//   - A struct must explicitly include the marker and a correctly typed field.
//   - The tool assumes all packages are located within a Go module (go.mod)
//
// Constants:
//   - typegenMarker: The marker comment used to signal code generation.
//   - generatedFile: Name of the generated output file.
//   - runtimeImport: Required import path for `runtime.Type`.
//   - runtimeTypeFieldName: The name of the field that holds the type information.
//
// Note:
//
//	This tool is intended to be run manually or integrated into a code generation step
//	in your CI/CD or build pipeline. The generated code should be committed if required
//	for downstream consumers.
package main
