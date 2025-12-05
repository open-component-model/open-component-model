package graph

//import (
//	"fmt"
//
//	invopop "github.com/invopop/jsonschema"
//	"ocm.software/open-component-model/bindings/go/cel/fieldpath"
//	"ocm.software/open-component-model/bindings/go/runtime"
//)
//
//type matchResult int
//
//const (
//	matchNone   matchResult = iota
//	matchPrefix             // desc shorter than typed, e.g. "spec" vs "spec.repository"
//	matchEqual              // desc == typed, e.g. "spec.repository"
//	matchChild              // desc == typed + one child, e.g. "spec.repository.type"
//)
//
//func matchSegments(typed, desc []fieldpath.Segment) matchResult {
//	// Descriptor can be shorter, equal, or exactly one segment longer than typed.
//	if len(desc) > len(typed)+1 {
//		return matchNone
//	}
//
//	// Compare left-to-right (prefix-based).
//	for i := 0; i < len(desc) && i < len(typed); i++ {
//		if typed[i].Name != desc[i].Name {
//			return matchNone
//		}
//	}
//
//	switch {
//	case len(desc) < len(typed):
//		return matchPrefix
//	case len(desc) == len(typed):
//		return matchEqual
//	case len(desc) == len(typed)+1:
//		return matchChild
//	default:
//		return matchNone
//	}
//}
//
//// navigate JSON schema along a relative path (descriptor -> typed field).
//// Only object properties supported. Indexes are rejected for now.
//func schemaAtRelativePath(root *invopop.Schema, rel []fieldpath.Segment) (*invopop.Schema, error) {
//	cur := root
//	for _, seg := range rel {
//		if seg.Index >= 0 {
//			return nil, fmt.Errorf("indexed segment not supported at %q[%d]", seg.Name, seg.Index)
//		}
//		prop, ok := cur.Properties.Get(seg.Name)
//		if !ok {
//			return nil, fmt.Errorf("schema missing property %q", seg.Name)
//		}
//		cur = prop
//	}
//	return cur, nil
//}
//
//// find discriminator "type" const at the schema node of the typed field.
//func discriminatorConstAt(node *invopop.Schema) (string, error) {
//	p, ok := node.Properties.Get(runtime.IdentityAttributeType)
//	if !ok {
//		return "", fmt.Errorf("missing discriminator property 'type'")
//	}
//	s, ok := p.Const.(string)
//	if !ok || s == "" {
//		if p.Enum != nil {
//			if s, ok = p.Enum[0].(string); ok {
//				return s, nil
//			}
//		}
//		return "", fmt.Errorf("'type' discriminator is not a const string or enum string")
//	}
//	return s, nil
//}
