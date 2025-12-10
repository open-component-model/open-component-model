package workerpool

// ResolutionEvent represents a notification that a component version resolution has completed.
type ResolutionEvent struct {
	// Component is the name of the component that was resolved.
	Component string
	// Version is the version of the component that was resolved.
	Version string
	// Error is set if the resolution failed.
	Error error
	// Requesters is a list of requesters that requested this resolution.
	// These will be enqueued for reconciliation when the resolution completes.
	Requesters []RequesterInfo
}
