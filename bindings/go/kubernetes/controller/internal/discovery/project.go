package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel/conversion"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Payload is the projected result of a Filtered view. Exactly one of the
// output fields is selected; the selected field is always non-nil, even when
// the result is empty.
type Payload struct {
	// Components carries the filtered raw v2 descriptors, sorted
	// lexicographically by (component.name, component.version). Selected when
	// the Discovery has no extract configuration.
	Components []json.RawMessage
	// Extracted carries the projected records. Selected when the Discovery
	// has an extract configuration.
	Extracted []map[string]any
	// EmptyStage is the stage that emptied the result, carried over from
	// Filter. It is empty for an ordinary result.
	EmptyStage string
}

// Project projects a filtered view into its payload. Serialization happens only
// here: each surviving descriptor is converted to v2 and marshalled once per
// invocation. Raw mode marshals the v2 descriptor directly; extraction mode
// additionally decodes it into a generic map for CEL evaluation.
//
// Descriptor conversion, marshalling, or decoding failures are returned as
// ordinary wrapped errors carrying the component name/version, not as
// *ExtractError, so they are not reported as configuration failures.
//
// An empty filtered view deterministically produces an empty selected list:
// whole-expression extraction must not fabricate records in that state, so
// expressions are not evaluated at all.
//
// byResources emits one record per surviving (component, resource) pair and
// byComponents one record per surviving component, iterating fields in
// lexicographic order. Fields whose expression accesses missing data are
// omitted; the per-iteration record is kept even when all its fields
// disappear. Expression mode evaluates once over the complete filtered
// descriptor list and must produce a list of objects with string keys; record
// order produced by the expression is retained.
func (q *Query) Project(ctx context.Context, filtered *Filtered) (*Payload, error) {
	if filtered == nil {
		// Never a computed-empty result: status distinguishes an absent payload
		// from an empty one, so a nil view must not publish "found nothing".
		return nil, fmt.Errorf("filtered view must not be nil")
	}

	if q.extract == nil {
		payload := &Payload{Components: make([]json.RawMessage, 0, len(filtered.Descriptors)), EmptyStage: filtered.EmptyStage}
		if len(filtered.Descriptors) == 0 {
			return payload, nil
		}
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		for _, d := range filtered.Descriptors {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			raw, err := marshalV2(scheme, d)
			if err != nil {
				return nil, err
			}
			payload.Components = append(payload.Components, raw)
		}
		return payload, nil
	}

	payload := &Payload{Extracted: make([]map[string]any, 0), EmptyStage: filtered.EmptyStage}
	if len(filtered.Descriptors) == 0 {
		return payload, nil
	}

	descriptors, err := unstructuredDescriptors(ctx, filtered.Descriptors)
	if err != nil {
		return nil, err
	}

	// Every projector returns an allocated slice, so the selected output stays
	// non-nil even when empty. The default keeps a future mode from shipping as
	// a silent empty result.
	switch q.extract.mode {
	case extractByResources:
		payload.Extracted, err = q.projectPerResource(ctx, descriptors)
	case extractByComponents:
		payload.Extracted, err = q.projectPerComponent(ctx, descriptors)
	case extractExpression:
		payload.Extracted, err = q.projectExpression(ctx, descriptors)
	default:
		return nil, extractErrorf("", "unknown extraction mode %d", q.extract.mode)
	}
	if err != nil {
		return nil, err
	}

	return payload, nil
}

// marshalV2 converts a runtime descriptor to v2 and marshals it as deterministic
// JSON. Conversion and marshalling failures carry the component name/version.
func marshalV2(scheme *runtime.Scheme, d *descriptor.Descriptor) (json.RawMessage, error) {
	v2desc, err := descriptor.ConvertToV2(scheme, d)
	if err != nil {
		return nil, fmt.Errorf("failed to convert descriptor %s to v2: %w", d.Component.String(), err)
	}
	raw, err := json.Marshal(v2desc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal descriptor %s: %w", d.Component.String(), err)
	}
	return raw, nil
}

// unstructuredDescriptors converts each descriptor to v2, marshals it, and
// decodes it into a runtime.Unstructured for CEL evaluation, once per Project
// invocation. A single allow-unknown scheme is reused for all descriptors.
func unstructuredDescriptors(ctx context.Context, descriptors []*descriptor.Descriptor) ([]*runtime.Unstructured, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	out := make([]*runtime.Unstructured, 0, len(descriptors))
	for _, d := range descriptors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		raw, err := marshalV2(scheme, d)
		if err != nil {
			return nil, err
		}
		generic := &runtime.Unstructured{}
		if err := json.Unmarshal(raw, generic); err != nil {
			return nil, fmt.Errorf("failed to unmarshal descriptor %s into generic map: %w", d.Component.String(), err)
		}
		out = append(out, generic)
	}
	return out, nil
}

// component returns the inner component map of a full v2 descriptor.
// Do not ignore the missing component field.
func component(desc *runtime.Unstructured) (map[string]any, error) {
	c, ok := runtime.Get[map[string]any](desc, "component")
	if !ok {
		return nil, fmt.Errorf("descriptor has no component object")
	}

	return c, nil
}

// resources returns the resource maps of an inner component map. It handles
// both a nil and an empty resource list.
func resources(component map[string]any) []map[string]any {
	list, ok := component["resources"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, res := range list {
		if m, ok := res.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (q *Query) projectPerResource(ctx context.Context, descriptors []*runtime.Unstructured) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(descriptors))
	for i, desc := range descriptors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		comp, err := component(desc)
		if err != nil {
			return nil, fmt.Errorf("failed to project resources of descriptor %d: %w", i, err)
		}
		for _, resource := range resources(comp) {
			record, err := q.evalFields(ctx, map[string]any{
				"component": comp,
				"resource":  resource,
			})
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func (q *Query) projectPerComponent(ctx context.Context, descriptors []*runtime.Unstructured) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(descriptors))
	for i, desc := range descriptors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		comp, err := component(desc)
		if err != nil {
			return nil, fmt.Errorf("failed to project descriptor %d: %w", i, err)
		}
		record, err := q.evalFields(ctx, map[string]any{"component": comp})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (q *Query) projectExpression(ctx context.Context, descriptors []*runtime.Unstructured) ([]map[string]any, error) {
	components := make([]any, 0, len(descriptors))
	for _, desc := range descriptors {
		components = append(components, desc.Data)
	}
	val, _, err := q.extract.expression.ContextEval(ctx, map[string]any{"components": components})
	// Missing access is a strict error for whole-expression extraction, unlike
	// map-field extraction where it only omits the field, so both outcomes of
	// evalResult are handled the same way here.
	if _, cause := evalResult(ctx, val, err); cause != nil {
		return nil, extractErrorf("", "failed to evaluate expression: %w", cause)
	}
	native, err := conversion.GoNativeType(val)
	if err != nil {
		return nil, extractErrorf("", "failed to convert expression result: %w", err)
	}
	list, ok := native.([]any)
	if !ok {
		return nil, extractErrorf("", "expression must evaluate to a list of objects, got %T", native)
	}
	records := make([]map[string]any, 0, len(list))
	for _, item := range list {
		record, ok := item.(map[string]any)
		if !ok {
			return nil, extractErrorf("", "expression must evaluate to a list of objects, got element of type %T", item)
		}
		records = append(records, record)
	}
	return records, nil
}

// evalFields evaluates one compiled extraction map against an activation.
// Fields are compiled in lexicographic order, so errors are deterministic.
// A field whose expression accesses missing data is omitted; the record is
// kept even when all its fields disappear. Genuine CEL errors are returned as
// *ExtractError.
func (q *Query) evalFields(ctx context.Context, activation map[string]any) (map[string]any, error) {
	record := make(map[string]any, len(q.extract.fields))
	for _, field := range q.extract.fields {
		val, _, err := field.prog.ContextEval(ctx, activation)
		missing, cause := evalResult(ctx, val, err)
		if missing {
			continue
		}
		if cause != nil {
			return nil, extractErrorf(field.name, "failed to evaluate expression: %w", cause)
		}
		native, err := conversion.GoNativeType(val)
		if err != nil {
			return nil, extractErrorf(field.name, "failed to convert expression result: %w", err)
		}
		record[field.name] = native
	}
	return record, nil
}
