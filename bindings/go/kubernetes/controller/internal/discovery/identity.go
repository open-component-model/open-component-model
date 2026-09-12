package discovery

import (
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// referenceIdentity returns the selector identity of a component reference: the
// reference element identity (local name, version, extra identity) plus the
// "componentName" attribute holding the referenced component's name.
// Reference.ToComponentIdentity alone would lose the local name and extras.
func referenceIdentity(ref *descriptor.Reference) runtime.Identity {
	identity := ref.ToIdentity()
	if identity == nil {
		identity = runtime.Identity{}
	}
	identity["componentName"] = ref.Component
	return identity
}

// componentIdentity returns the selector identity of a component.
func componentIdentity(component *descriptor.Component) runtime.Identity {
	return component.ToIdentity()
}

// resourceIdentity returns the selector identity of a resource.
func resourceIdentity(res *descriptor.Resource) runtime.Identity {
	return res.ToIdentity()
}

// labelValues materializes OCM labels as a label-name to decoded-JSON-value map.
// Decoding errors (for example, values that are not valid YAML) skip the label.
// With duplicate label names the last label wins.
func labelValues(labels []descriptor.Label) map[string]any {
	m := make(map[string]any, len(labels))
	for i := range labels {
		var v any
		if err := labels[i].GetValue(&v); err != nil {
			continue
		}
		m[labels[i].Name] = v
	}
	return m
}

// matchIdentity returns true when the identity contains every requested
// key-value pair. Key presence is required, also for comparisons against "".
func matchIdentity(match map[string]string, identity runtime.Identity) bool {
	for k, want := range match {
		got, ok := identity[k]
		if !ok || got != want {
			return false
		}
	}
	return true
}

// matchLabels returns true when every requested label is present with the
// requested string value. Non-string (structured) label values never match;
// they remain selectable via CEL expressions.
func matchLabels(match map[string]string, labels map[string]any) bool {
	for name, want := range match {
		got, ok := labels[name]
		if !ok {
			return false
		}
		s, ok := got.(string)
		if !ok || s != want {
			return false
		}
	}
	return true
}
