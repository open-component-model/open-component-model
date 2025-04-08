package annotations

// Annotations for OCI Image Manifests
const (
	// OCMComponentVersion is an annotation that indicates the component version.
	// It is an annotation that can be used during referrer resolution to identify the component version.
	// Do not modify this otuside of the OCM binding library
	OCMComponentVersion = "software.ocm.componentversion"

	// OCMCreator is an annotation that indicates the creator of the component version.
	// It is used historically by the OCM CLI to indicate the creator of the component version.
	// It is usually only a meta information, and has no semantic meaning beyond identifying a creating
	// process or user agent. as such it CAN be correlated to a user agent header in http.
	OCMCreator = "software.ocm.creator"
)
