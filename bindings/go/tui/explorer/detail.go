package explorer

import (
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// NodeDetail returns a formatted string with details about the selected node.
func NodeDetail(n *Node) string {
	if n == nil {
		return "Select a node to view details."
	}

	switch n.Kind {
	case NodeComponent:
		return fmt.Sprintf("Component: %s", n.Label)

	case NodeVersion:
		return versionDetail(n)

	case NodeResource:
		return resourceDetail(n.Resource)

	case NodeSource:
		return sourceDetail(n.Source)

	case NodeReference:
		return referenceDetail(n.Reference)

	case NodeSignature:
		return signatureDetail(n.Signature)

	case NodeResourceGroup, NodeSourceGroup, NodeReferenceGroup,
		NodeSignatureGroup, NodeLabelGroup:
		return fmt.Sprintf("%s\n\nPress enter to expand.", n.Label)

	case NodeLabel:
		return fmt.Sprintf("Label: %s", n.Label)

	default:
		return n.Label
	}
}

func versionDetail(n *Node) string {
	if n.Descriptor == nil {
		return fmt.Sprintf("Version: %s", n.Label)
	}
	desc := n.Descriptor
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Name:      %s\n", desc.Component.Name))
	b.WriteString(fmt.Sprintf("Version:   %s\n", desc.Component.Version))
	if desc.Component.Provider.Name != "" {
		b.WriteString(fmt.Sprintf("Provider:  %s\n", desc.Component.Provider.Name))
	}
	if desc.Component.CreationTime != "" {
		b.WriteString(fmt.Sprintf("Created:   %s\n", desc.Component.CreationTime))
	}
	if desc.Meta.Version != "" {
		b.WriteString(fmt.Sprintf("Schema:    %s\n", desc.Meta.Version))
	}
	b.WriteString(fmt.Sprintf("\nResources:  %d\n", len(desc.Component.Resources)))
	b.WriteString(fmt.Sprintf("Sources:    %d\n", len(desc.Component.Sources)))
	b.WriteString(fmt.Sprintf("References: %d\n", len(desc.Component.References)))
	b.WriteString(fmt.Sprintf("Signatures: %d\n", len(desc.Signatures)))
	if len(desc.Component.Labels) > 0 {
		b.WriteString(fmt.Sprintf("Labels:     %d\n", len(desc.Component.Labels)))
	}
	return b.String()
}

func resourceDetail(res *descriptor.Resource) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Name:     %s\n", res.Name))
	b.WriteString(fmt.Sprintf("Version:  %s\n", res.Version))
	b.WriteString(fmt.Sprintf("Type:     %s\n", res.Type))
	b.WriteString(fmt.Sprintf("Relation: %s\n", res.Relation))
	if res.Digest != nil {
		b.WriteString(fmt.Sprintf("\nDigest:\n"))
		b.WriteString(fmt.Sprintf("  Algorithm:      %s\n", res.Digest.HashAlgorithm))
		b.WriteString(fmt.Sprintf("  Normalisation:  %s\n", res.Digest.NormalisationAlgorithm))
		b.WriteString(fmt.Sprintf("  Value:          %s\n", res.Digest.Value))
	}
	if len(res.Labels) > 0 {
		b.WriteString(fmt.Sprintf("\nLabels:\n"))
		for _, l := range res.Labels {
			b.WriteString(fmt.Sprintf("  %s: %s\n", l.Name, string(l.Value)))
		}
	}
	return b.String()
}

func sourceDetail(src *descriptor.Source) string {
	if src == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Name:    %s\n", src.Name))
	b.WriteString(fmt.Sprintf("Version: %s\n", src.Version))
	b.WriteString(fmt.Sprintf("Type:    %s\n", src.Type))
	if len(src.Labels) > 0 {
		b.WriteString(fmt.Sprintf("\nLabels:\n"))
		for _, l := range src.Labels {
			b.WriteString(fmt.Sprintf("  %s: %s\n", l.Name, string(l.Value)))
		}
	}
	return b.String()
}

func referenceDetail(ref *descriptor.Reference) string {
	if ref == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Name:      %s\n", ref.Name))
	b.WriteString(fmt.Sprintf("Component: %s\n", ref.Component))
	b.WriteString(fmt.Sprintf("Version:   %s\n", ref.Version))
	if ref.Digest.Value != "" {
		b.WriteString(fmt.Sprintf("\nDigest:\n"))
		b.WriteString(fmt.Sprintf("  Algorithm:      %s\n", ref.Digest.HashAlgorithm))
		b.WriteString(fmt.Sprintf("  Normalisation:  %s\n", ref.Digest.NormalisationAlgorithm))
		b.WriteString(fmt.Sprintf("  Value:          %s\n", ref.Digest.Value))
	}
	if len(ref.Labels) > 0 {
		b.WriteString(fmt.Sprintf("\nLabels:\n"))
		for _, l := range ref.Labels {
			b.WriteString(fmt.Sprintf("  %s: %s\n", l.Name, string(l.Value)))
		}
	}
	return b.String()
}

func signatureDetail(sig *descriptor.Signature) string {
	if sig == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Name:      %s\n", sig.Name))
	b.WriteString(fmt.Sprintf("Algorithm: %s\n", sig.Signature.Algorithm))
	if sig.Signature.MediaType != "" {
		b.WriteString(fmt.Sprintf("MediaType: %s\n", sig.Signature.MediaType))
	}
	if sig.Signature.Issuer != "" {
		b.WriteString(fmt.Sprintf("Issuer:    %s\n", sig.Signature.Issuer))
	}
	b.WriteString(fmt.Sprintf("\nDigest:\n"))
	b.WriteString(fmt.Sprintf("  Algorithm:      %s\n", sig.Digest.HashAlgorithm))
	b.WriteString(fmt.Sprintf("  Normalisation:  %s\n", sig.Digest.NormalisationAlgorithm))
	b.WriteString(fmt.Sprintf("  Value:          %s\n", sig.Digest.Value))
	return b.String()
}
