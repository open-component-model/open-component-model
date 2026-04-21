// Package fetch defines the interfaces for retrieving OCM component data.
// Implementations are provided by the CLI layer which has access to the
// plugin manager and credential graph.
package fetch

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// ComponentFetcher abstracts component version retrieval for the TUI.
type ComponentFetcher interface {
	// ListVersions returns all available versions for a component.
	ListVersions(ctx context.Context, component string) ([]string, error)
	// GetDescriptor returns the full descriptor for a component version.
	GetDescriptor(ctx context.Context, component, version string) (*descriptor.Descriptor, error)
}

// FetcherFactory creates a ComponentFetcher from a parsed component reference.
// The reference string is the raw user input (e.g. "ghcr.io/org/repo//component:version").
// It returns the fetcher, the parsed component name, the parsed version (may be empty),
// and any error.
type FetcherFactory func(ctx context.Context, reference string) (fetcher ComponentFetcher, component string, version string, err error)

// TransferOptions holds the user-selected options for a transfer operation.
type TransferOptions struct {
	Recursive     bool
	CopyResources bool
	UploadAs      string // "default", "localBlob", "ociArtifact"
}

// TransferExecutor abstracts the transfer workflow for the TUI.
type TransferExecutor interface {
	// BuildGraph builds a transformation graph definition for review.
	BuildGraph(ctx context.Context, source, target string, opts TransferOptions) (*transformv1alpha1.TransformationGraphDefinition, error)
	// Execute runs a previously built transformation graph.
	Execute(ctx context.Context, tgd *transformv1alpha1.TransformationGraphDefinition, progress chan<- TransferProgress) error
}

// TransferProgress reports the progress of a transfer execution.
type TransferProgress struct {
	Step    string
	Total   int
	Current int
	Done    bool
	Err     error
	IsLog   bool // true if this is a slog message rather than a progress event
}
