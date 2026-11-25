package attestation

import (
	"slices"
	"strings"

	"ocm.software/open-component-model/bindings/go/repository"
)

// Friendly type vocabulary, inlined because the unopinionated model defines no type
// constants of its own. Used only to widen --type matching and to shape display, never to
// classify or drop.
const (
	predicateOpenVEX   = "https://openvex.dev/ns"
	predicateSLSA      = "https://slsa.dev/provenance/"
	predicateCosignSig = "https://sigstore.dev/cosign/sign/v1"
	mediaOpenVEX       = "application/openvex+json"
	mediaOpenVEXVnd    = "application/vnd.openvex+json"
	mediaSARIF         = "application/sarif+json"
	artifactCosignSig  = "application/vnd.dev.cosign.artifact.sig.v1+json"
	artifactNotation   = "application/vnd.cncf.notary.signature"
	resourceTypeSBOM   = "sbom"
	resourceTypeVEX    = "vex"
)

// Classify derives the friendly category and format of an attestation from its raw type
// signals. It is advisory display sugar for the tree, the --type aliases and output file
// names, never stored and never a gate. An unrecognised attestation yields an empty
// category and its raw type as the format, so nothing is hidden.
func Classify(a repository.Attestation) (category, format string) {
	pt, at, mt := a.PredicateType, a.ArtifactType, a.MediaType
	switch {
	case pt == repository.PredicateTypeSPDX || mt == repository.MediaTypeSPDXJSON:
		return "sbom", "spdx"
	case pt == repository.PredicateTypeCycloneDX || mt == repository.MediaTypeCycloneDXJSON:
		return "sbom", "cyclonedx"
	case strings.HasPrefix(pt, predicateOpenVEX) || mt == mediaOpenVEX || mt == mediaOpenVEXVnd:
		return "vex", "openvex"
	case strings.HasPrefix(pt, predicateSLSA):
		return "provenance", "slsa"
	case at == artifactCosignSig || pt == predicateCosignSig:
		return "signature", "cosign"
	case at == artifactNotation:
		return "signature", "notation"
	case at == mediaSARIF || mt == mediaSARIF:
		return "sarif", ""
	case at == resourceTypeSBOM:
		return "sbom", ""
	case at == resourceTypeVEX:
		return "vex", ""
	default:
		return "", a.Type()
	}
}

// TypeAliases lists the friendly filter tokens of an attestation (its format and
// category), used only to widen --type matching. An unrecognised attestation yields none,
// staying matchable by raw substring.
func TypeAliases(a repository.Attestation) []string {
	category, format := Classify(a)
	var out []string
	if format != "" && format != a.Type() {
		out = append(out, format)
	}
	if category != "" {
		out = append(out, category)
	}
	return out
}

// FilterByType keeps attestations whose raw type signals contain any of the given
// substrings (case-insensitive) or whose friendly aliases match one exactly. An empty
// filter keeps everything.
func FilterByType(atts []repository.Attestation, filters []string) []repository.Attestation {
	if len(filters) == 0 {
		return atts
	}
	norm := make([]string, len(filters))
	for i, f := range filters {
		norm[i] = strings.ToLower(strings.TrimSpace(f))
	}
	kept := make([]repository.Attestation, 0, len(atts))
	for _, a := range atts {
		haystack := strings.ToLower(strings.Join([]string{a.Type(), a.ArtifactType, a.PredicateType, a.MediaType}, "\x00"))
		if anyFilterMatches(norm, haystack, TypeAliases(a)) {
			kept = append(kept, a)
		}
	}
	return kept
}

func anyFilterMatches(filters []string, haystack string, aliases []string) bool {
	for _, f := range filters {
		if strings.Contains(haystack, f) || slices.Contains(aliases, f) {
			return true
		}
	}
	return false
}
