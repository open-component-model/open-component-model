package resources

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/jedib0t/go-pretty/v6/list"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Keys order identity attributes are grouped by in the tree output.
var treeAttributeGroupingOrder = []string{"version", "architecture", "os", "variant"}

func encodeResources(format string, descriptor *descruntime.Descriptor) (io.Reader, int64, error) {
	switch format {
	case "tree":
		return encodeResourcesTree(descriptor, treeOptions{})
	case "treewide":
		return encodeResourcesTree(descriptor, treeOptions{ShowDigest: true})
	default:
		return nil, 0, fmt.Errorf("unknown format: %s", format)
	}
}

type treeOptions struct {
	ShowDigest bool
}

func encodeResourcesTree(descriptor *descruntime.Descriptor, opts treeOptions) (io.Reader, int64, error) {
	var buf bytes.Buffer
	t := list.NewWriter()
	t.SetOutputMirror(&buf)
	t.AppendItem(descriptor.String())

	// Group by <name(version)>
	nameGroup := map[string][]descruntime.Resource{}
	for _, resource := range descriptor.Component.Resources {
		key := resource.Name
		nameGroup[key] = append(nameGroup[key], resource)
	}

	t.Indent()

	// Process each name(version) group
	for name, resources := range nameGroup {
		t.AppendItem(name)
		t.Indent()
		groupByKeys(t, resources, 0, opts) // start grouping at first key
		t.UnIndent()
	}
	t.UnIndent()

	t.SetStyle(list.StyleConnectedRounded)
	t.Render()
	return &buf, int64(buf.Len()), nil
}

func groupByKeys(t list.Writer, resources []descruntime.Resource, depth int, opts treeOptions) {
	if depth >= len(treeAttributeGroupingOrder) {
		// leaf level: show resources
		for _, resource := range resources {
			t.AppendItem("access: " + resource.Access.GetType().String())
			t.AppendItem("relation: " + resource.Relation)

			if opts.ShowDigest {
				t.AppendItem("digest")
				if dig := resource.Digest; dig != nil {
					t.Indent()
					t.AppendItem("value: " + dig.Value)
					t.AppendItem("normalization: " + dig.NormalisationAlgorithm)
					t.AppendItem("hash: " + dig.HashAlgorithm)
					t.UnIndent()
				}
			}
		}
		return
	}

	key := treeAttributeGroupingOrder[depth]

	// Group resources by this key
	group := map[string][]descruntime.Resource{}
	for _, r := range resources {
		id := r.ToIdentity()
		value := id[key]
		if value == "" {
			value = "<none>"
		}
		group[value] = append(group[value], r)
	}

	// Sorted group keys
	var values []string
	for v := range group {
		values = append(values, v)
	}
	sort.Strings(values)

	// Output each subgroup and recurse deeper
	for _, v := range values {
		if v == "<none>" {
			// Skip extra indent, just continue grouping deeper
			groupByKeys(t, group[v], depth+1, opts)
		} else {
			t.AppendItem(key + ": " + v)
			t.Indent()
			groupByKeys(t, group[v], depth+1, opts)
			t.UnIndent()
		}
	}
}
