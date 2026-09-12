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
	// Reason is the empty-stage reason carried over from Filter.
	Reason EmptyReason
}

// Project projects a filtered view into its payload. Serialization happens only
// here: each surviving descriptor is converted to v2 and marshalled once per
// invocation. Raw mode marshals the v2 descriptor directly; extraction mode
// additionally decodes it into a generic map for CEL evaluation.
//
// Descriptor conversion, marshalling, or decoding failures are returned as
// ordinary wrapped errors carrying the component name/version, not as
// *ExtractError, so the controller treats them as retryable rather than
// terminal configuration failures.
//
// An empty filtered view with a stage reason deterministically produces an
// empty selected list: whole-expression extraction must not fabricate records
// in that state, so expressions are not evaluated at all.
//
// byResources emits one record per surviving (component, resource) pair and
// byComponents one record per surviving component, iterating fields in
// lexicographic order. Fields whose expression accesses missing data are
// omitted; the per-iteration record is kept even when all its fields
// disappear. Expression mode evaluates once over the complete filtered
// descriptor list and must produce a list of objects with string keys; record
// order produced by the expression is retained.
func (q *Query) Project(ctx context.Context, filtered *Filtered) (*Payload, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if filtered == nil {
		filtered = &Filtered{}
	}

	empty := len(filtered.Descriptors) == 0
	if q.extract == nil {
		payload := &Payload{Components: make([]json.RawMessage, 0, len(filtered.Descriptors)), Reason: filtered.Reason}
		if empty {
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

	payload := &Payload{Extracted: make([]map[string]any, 0), Reason: filtered.Reason}
	if empty {
		return payload, nil
	}

	descriptors, err := descriptorMaps(ctx, filtered.Descriptors)
	if err != nil {
		return nil, err
	}

	switch q.extract.mode {
	case extractByResources:
		payload.Extracted, err = q.projectPerResource(ctx, descriptors)
	case extractByComponents:
		payload.Extracted, err = q.projectPerComponent(ctx, descriptors)
	case extractExpression:
		payload.Extracted, err = q.projectExpression(ctx, descriptors)
	}
	if err != nil {
		return nil, err
	}
	// A selected but empty extraction result is an empty list, never nil.
	if payload.Extracted == nil {
		payload.Extracted = []map[string]any{}
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

// descriptorMaps converts each descriptor to v2, marshals it, and decodes it
// into a generic map for CEL evaluation, once per Project invocation. A single
// allow-unknown scheme is reused for all descriptors.
func descriptorMaps(ctx context.Context, descriptors []*descriptor.Descriptor) ([]map[string]any, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	maps := make([]map[string]any, 0, len(descriptors))
	for _, d := range descriptors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		raw, err := marshalV2(scheme, d)
		if err != nil {
			return nil, err
		}
		var generic map[string]any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil, fmt.Errorf("failed to unmarshal descriptor %s into generic map: %w", d.Component.String(), err)
		}
		maps = append(maps, generic)
	}
	return maps, nil
}

// component returns the inner component map of a full v2 descriptor map.
func component(desc map[string]any) map[string]any {
	if c, ok := desc["component"].(map[string]any); ok {
		return c
	}
	return nil
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

func (q *Query) projectPerResource(ctx context.Context, descriptors []map[string]any) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(descriptors))
	for _, desc := range descriptors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		comp := component(desc)
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

func (q *Query) projectPerComponent(ctx context.Context, descriptors []map[string]any) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(descriptors))
	for _, desc := range descriptors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		record, err := q.evalFields(ctx, map[string]any{"component": component(desc)})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (q *Query) projectExpression(ctx context.Context, descriptors []map[string]any) ([]map[string]any, error) {
	components := make([]any, 0, len(descriptors))
	for _, desc := range descriptors {
		components = append(components, desc)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	val, _, err := q.extract.expression.ContextEval(ctx, map[string]any{"components": components})
	// Missing access is a strict error for whole-expression extraction,
	// unlike map-field extraction where it only omits the field.
	if missing, cause := evalResult(val, err); missing {
		return nil, extractErrorf("", "failed to evaluate expression: %s", cause)
	} else if cause != nil {
		return nil, extractErrorf("", "failed to evaluate expression: %s", cause)
	}
	native, err := conversion.GoNativeType(val)
	if err != nil {
		return nil, extractErrorf("", "failed to convert expression result: %s", err)
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
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		val, _, err := field.prog.ContextEval(ctx, activation)
		missing, cause := evalResult(val, err)
		if missing {
			continue
		}
		if cause != nil {
			return nil, extractErrorf(field.name, "failed to evaluate expression: %s", cause)
		}
		native, err := conversion.GoNativeType(val)
		if err != nil {
			return nil, extractErrorf(field.name, "failed to convert expression result: %s", err)
		}
		record[field.name] = native
	}
	return record, nil
}
