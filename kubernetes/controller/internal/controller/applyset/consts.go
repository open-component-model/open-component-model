package applyset

const (
	// ApplySetIDPartDelimiter is the delimiter used when constructing an ApplySet ID.
	ApplySetIDPartDelimiter = "."

	// V1ApplySetIdFormat is the format string for v1 ApplySet IDs.
	V1ApplySetIdFormat = "applyset-%s"

	// ApplySetParentIDLabel is the label applied to parent objects to identify an ApplySet.
	// The value is the unique ID for the ApplySet.
	ApplySetParentIDLabel = "applyset.kubernetes.io.io/id"

	// ApplySetPartOfLabel is the label applied to member objects to indicate they are part of an ApplySet.
	// The value matches the ApplySet ID on the parent appliedObject.
	ApplySetPartOfLabel = "applyset.kubernetes.io.io/part-of"

	// ApplySetToolingLabel is the label on the parent appliedObject indicating which tool is managing the ApplySet.
	ApplySetToolingLabel = "applyset.kubernetes.io.io/tooling"

	// ApplySetGKsAnnotation is an optional "hint" annotation listing all GroupKinds in the ApplySet.
	// This helps optimize discovery of member objects.
	ApplySetGKsAnnotation = "applyset.kubernetes.io.io/contains-group-kinds"

	// ApplySetAdditionalNamespacesAnnotation extends the scope of an ApplySet to include additional namespaces.
	ApplySetAdditionalNamespacesAnnotation = "applyset.kubernetes.io.io/additional-namespaces"

	maxConcurrency = 10
)
