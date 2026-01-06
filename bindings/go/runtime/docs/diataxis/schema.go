package diataxis

// Diataxis (Diátaxis) is A systematic approach to technical documentation authoring.
//
// Diátaxis is a way of thinking about and doing documentation.
//
// It prescribes approaches to content, architecture and form
// that emerge from a systematic approach to understanding the needs of documentation users.
//
// Diátaxis identifies four distinct needs, and four corresponding forms of documentation:
//   - tutorials
//   - how-to guides,
//   - technical reference
//   - explanation.
//
// It places them in a systematic relationship,
// and proposes that documentation should itself be organised around the structures of those needs.
//
// OCM assumes Diataxis can be attached to any technical reference documentation.
//
// see https://diataxis.fr/how-to-guides/
// +ocm:jsonschema-gen=true
type Diataxis struct {
	Tutorial    []DocRef `json:"tutorial,omitempty"`
	HowTo       []DocRef `json:"howto,omitempty"`
	Explanation []DocRef `json:"explanation,omitempty"`
}

type DocRef struct {
	ID       string   `json:"id,omitempty"`
	Content  string   `json:"content,omitempty"`
	Audience []string `json:"audience,omitempty"`
}
