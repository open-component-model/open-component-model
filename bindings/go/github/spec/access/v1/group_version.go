package v1

const (
	Version    = "v1"
	Type       = "gitHub"
	LegacyType = "github"
	// UpperType is the capitalized alias registered by old OCM
	// (github.com/open-component-model/ocm). Kept so component descriptors
	// written with type "GitHub"/"GitHub/v1" keep resolving.
	UpperType = "GitHub"
)
