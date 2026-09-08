package discovery

import (
	"context"
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel/conversion"
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

// Project projects a filtered view into its payload. An empty filtered view
// with a stage reason deterministically produces an empty selected list:
// whole-expression extraction must not fabricate records in that state,
// so expressions are not evaluated at all.
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

	empty := len(filtered.Components) == 0
	if q.extract == nil {
		payload := &Payload{Components: make([]json.RawMessage, 0, len(filtered.Components)), Reason: filtered.Reason}
		if empty {
			return payload, nil
		}
		for i := range filtered.Components {
			payload.Components = append(payload.Components, filtered.Components[i].Raw)
		}
		return payload, nil
	}

	payload := &Payload{Extracted: make([]map[string]any, 0), Reason: filtered.Reason}
	if empty {
		return payload, nil
	}

	var err error
	switch q.extract.mode {
	case extractByResources:
		payload.Extracted, err = q.projectPerResource(ctx, filtered.Components)
	case extractByComponents:
		payload.Extracted, err = q.projectPerComponent(ctx, filtered.Components)
	case extractExpression:
		payload.Extracted, err = q.projectExpression(ctx, filtered.Components)
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

func (q *Query) projectPerResource(ctx context.Context, components []FilteredComponent) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(components))
	for i := range components {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		for _, resource := range components[i].Resources {
			record, err := q.evalFields(ctx, map[string]any{
				"component": components[i].Component,
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

func (q *Query) projectPerComponent(ctx context.Context, components []FilteredComponent) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(components))
	for i := range components {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		record, err := q.evalFields(ctx, map[string]any{"component": components[i].Component})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (q *Query) projectExpression(ctx context.Context, components []FilteredComponent) ([]map[string]any, error) {
	descriptors := make([]any, 0, len(components))
	for i := range components {
		descriptors = append(descriptors, components[i].Descriptor)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	val, _, err := q.extract.expression.ContextEval(ctx, map[string]any{"components": descriptors})
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
