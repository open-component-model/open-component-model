package repository

import "log/slog"

// ComponentListerOptions defines generic options that should be supported
// by implementations of the 'ComponentLister' interface.
type ComponentListerOptions struct {
	// NameListPageSize specifies the page size that should be used.
	// If zero or not set, the complete list is returned.
	NameListPageSize int

	// SortAlphabetically specifies whether the returned list should be sorted alphabetically.
	// If false or not set, the order returned by the underlying store is preserved.
	SortAlphabetically bool

	// Logger is the logger to use in the lister.
	// If not provided, a default one will be used.
	Logger *slog.Logger
}

type ComponentListerOption func(*ComponentListerOptions)

// WithPageSize sets the page size that is to be used by the component lister.
func WithPageSize(size int) ComponentListerOption {
	return func(o *ComponentListerOptions) {
		o.NameListPageSize = size
	}
}

// WithSortAlphabetically specifies, if the returned list should be alphabetically sorted.
// Default is false, i.e. the order returned by the underlying store implementation is preserved.
func WithSortAlphabetically(sort bool) ComponentListerOption {
	return func(o *ComponentListerOptions) {
		o.SortAlphabetically = sort
	}
}

// WithLogger sets the logger for the component lister.
func WithLogger(logger *slog.Logger) ComponentListerOption {
	return func(o *ComponentListerOptions) {
		o.Logger = logger
	}
}
