package v1

const (
	Version = "v1"
	// Type is the canonical access type, using the GitHub brand casing.
	Type = "GitHub"
	// LegacyType is the lowercase alias registered by old OCM
	// (github.com/open-component-model/ocm). Kept so component descriptors
	// written with type "github"/"github/v1" keep resolving.
	LegacyType = "github"
)
