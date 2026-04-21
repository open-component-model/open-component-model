package explorer

import (
	"fmt"
	"slices"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NodeKind identifies what type of tree node we're looking at.
type NodeKind int

const (
	NodeComponent NodeKind = iota
	NodeVersion
	NodeResourceGroup
	NodeResource
	NodeSourceGroup
	NodeSource
	NodeReferenceGroup
	NodeReference
	NodeSignatureGroup
	NodeSignature
	NodeLabelGroup
	NodeLabel
)

// Node represents a single item in the tree.
type Node struct {
	Kind     NodeKind
	Label    string
	Depth    int
	Expanded bool
	Loading  bool
	Children []*Node

	// Data associated with this node (set for leaf/detail nodes).
	Descriptor *descriptor.Descriptor
	Resource   *descriptor.Resource
	Source     *descriptor.Source
	Reference  *descriptor.Reference
	Signature  *descriptor.Signature
}

// IsExpandable returns true if this node can have children.
func (n *Node) IsExpandable() bool {
	switch n.Kind {
	case NodeComponent, NodeVersion, NodeResourceGroup, NodeSourceGroup,
		NodeReferenceGroup, NodeSignatureGroup, NodeLabelGroup, NodeReference:
		return true
	default:
		return false
	}
}

// Toggle flips the expanded state of an expandable node.
func (n *Node) Toggle() {
	if n.IsExpandable() {
		n.Expanded = !n.Expanded
	}
}

// Flatten returns a flat slice of visible nodes (respecting expand/collapse).
func Flatten(roots []*Node) []*Node {
	var result []*Node
	for _, root := range roots {
		flatten(root, &result)
	}
	return result
}

func flatten(n *Node, result *[]*Node) {
	*result = append(*result, n)
	if n.Expanded {
		for _, child := range n.Children {
			flatten(child, result)
		}
	}
}

// BuildVersionNodes creates children for a component node from a descriptor.
func BuildVersionNodes(desc *descriptor.Descriptor, depth int) []*Node {
	versionNode := &Node{
		Kind:       NodeVersion,
		Label:      desc.Component.Version,
		Depth:      depth,
		Expanded:   false,
		Descriptor: desc,
	}

	var groups []*Node

	if len(desc.Component.Resources) > 0 {
		resGroup := &Node{
			Kind:  NodeResourceGroup,
			Label: fmt.Sprintf("Resources (%d)", len(desc.Component.Resources)),
			Depth: depth + 1,
		}
		for i := range desc.Component.Resources {
			res := &desc.Component.Resources[i]
			resGroup.Children = append(resGroup.Children, &Node{
				Kind:     NodeResource,
				Label:    elementLabel(res.Name, res.Type, res.ExtraIdentity),
				Depth:    depth + 2,
				Resource: res,
			})
		}
		groups = append(groups, resGroup)
	}

	if len(desc.Component.Sources) > 0 {
		srcGroup := &Node{
			Kind:  NodeSourceGroup,
			Label: fmt.Sprintf("Sources (%d)", len(desc.Component.Sources)),
			Depth: depth + 1,
		}
		for i := range desc.Component.Sources {
			src := &desc.Component.Sources[i]
			srcGroup.Children = append(srcGroup.Children, &Node{
				Kind:   NodeSource,
				Label:  elementLabel(src.Name, src.Type, src.ExtraIdentity),
				Depth:  depth + 2,
				Source: src,
			})
		}
		groups = append(groups, srcGroup)
	}

	if len(desc.Component.References) > 0 {
		refGroup := &Node{
			Kind:  NodeReferenceGroup,
			Label: fmt.Sprintf("References (%d)", len(desc.Component.References)),
			Depth: depth + 1,
		}
		for i := range desc.Component.References {
			ref := &desc.Component.References[i]
			refGroup.Children = append(refGroup.Children, &Node{
				Kind:      NodeReference,
				Label:     fmt.Sprintf("%s -> %s:%s", ref.Name, ref.Component, ref.Version),
				Depth:     depth + 2,
				Reference: ref,
			})
		}
		groups = append(groups, refGroup)
	}

	if len(desc.Signatures) > 0 {
		sigGroup := &Node{
			Kind:  NodeSignatureGroup,
			Label: fmt.Sprintf("Signatures (%d)", len(desc.Signatures)),
			Depth: depth + 1,
		}
		for i := range desc.Signatures {
			sig := &desc.Signatures[i]
			label := sig.Name
			if sig.Signature.Algorithm != "" {
				label += fmt.Sprintf(" [%s]", sig.Signature.Algorithm)
			}
			sigGroup.Children = append(sigGroup.Children, &Node{
				Kind:      NodeSignature,
				Label:     label,
				Depth:     depth + 2,
				Signature: sig,
			})
		}
		groups = append(groups, sigGroup)
	}

	if len(desc.Component.Labels) > 0 {
		lblGroup := &Node{
			Kind:  NodeLabelGroup,
			Label: fmt.Sprintf("Labels (%d)", len(desc.Component.Labels)),
			Depth: depth + 1,
		}
		for _, lbl := range desc.Component.Labels {
			lblGroup.Children = append(lblGroup.Children, &Node{
				Kind:  NodeLabel,
				Label: fmt.Sprintf("%s = %s", lbl.Name, string(lbl.Value)),
				Depth: depth + 2,
			})
		}
		groups = append(groups, lblGroup)
	}

	versionNode.Children = groups
	return []*Node{versionNode}
}

// elementLabel builds a tree label like "ocmcli [executable] (os=linux, arch=amd64)".
func elementLabel(name, typ string, extraIdentity runtime.Identity) string {
	label := fmt.Sprintf("%s [%s]", name, typ)
	if len(extraIdentity) == 0 {
		return label
	}
	keys := make([]string, 0, len(extraIdentity))
	for k := range extraIdentity {
		if k == "name" || k == "version" {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, extraIdentity[k]))
	}
	if len(parts) > 0 {
		label += " (" + strings.Join(parts, ", ") + ")"
	}
	return label
}

// RenderNode produces a single-line string representation of a tree node.
func RenderNode(n *Node, isCursor bool) string {
	indent := strings.Repeat("  ", n.Depth)

	var prefix string
	switch {
	case n.Loading:
		prefix = "~ "
	case n.IsExpandable() && n.Expanded:
		prefix = "v "
	case n.IsExpandable():
		prefix = "> "
	default:
		prefix = "  "
	}

	line := indent + prefix + n.Label

	if isCursor {
		line = indent + prefix + n.Label
	}

	return line
}
