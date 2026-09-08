package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

// Selector stages.
const (
	StageReference = "reference"
	StageComponent = "component"
	StageResource  = "resource"
)

// SelectorError reports a selector compilation or evaluation failure at a stage.
type SelectorError struct {
	Stage string
	Cause error
}

func (e *SelectorError) Error() string {
	return fmt.Sprintf("%s selector: %s", e.Stage, e.Cause)
}

func (e *SelectorError) Unwrap() error {
	return e.Cause
}

func selectorErrorf(stage, format string, args ...any) *SelectorError {
	return &SelectorError{Stage: stage, Cause: fmt.Errorf(format, args...)}
}

// ExtractError reports an extraction compilation, evaluation, or output-type failure.
// Field names the map field for byResources/byComponents modes and is empty for
// expression mode.
type ExtractError struct {
	Field string
	Cause error
}

func (e *ExtractError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("extract field %q: %s", e.Field, e.Cause)
	}
	return fmt.Sprintf("extract: %s", e.Cause)
}

func (e *ExtractError) Unwrap() error {
	return e.Cause
}

func extractErrorf(field, format string, args ...any) *ExtractError {
	return &ExtractError{Field: field, Cause: fmt.Errorf(format, args...)}
}

// EmptyReason distinguishes an empty selector-stage result from an error. Its
// values are the corresponding condition reasons of the Discovery API.
type EmptyReason string

const (
	// EmptyReasonNone indicates a nonempty or selector-free result.
	EmptyReasonNone EmptyReason = ""
	// EmptyReasonNoReferencesMatched indicates the reference selector stage matched no reference.
	EmptyReasonNoReferencesMatched = EmptyReason(v1alpha1.NoReferencesMatchedReason)
	// EmptyReasonNoComponentsMatched indicates the component selector stage matched no component.
	EmptyReasonNoComponentsMatched = EmptyReason(v1alpha1.NoComponentsMatchedReason)
)

// isMissingAccess reports whether err is a CEL attribute-resolution failure,
// i.e. access to a missing map key or attribute. cel-go v0.31 does not export
// these error types, so detection relies on the message prefix. Genuine CEL
// errors such as "no such overload" must never match. Pinned by unit tests.
func isMissingAccess(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "no such key:") || strings.HasPrefix(msg, "no such attribute")
}

// evalResult classifies the result of a CEL evaluation. Missing attribute or
// key access is reported as missing, with the original error retained as
// cause for callers that treat missing access as an error. Context
// cancellation and all other CEL errors are reported as failures.
func evalResult(val ref.Val, err error) (missing bool, cause error) {
	if err != nil {
		if isMissingAccess(err) {
			return true, err
		}
		return false, err
	}
	if types.IsError(val) {
		e, ok := val.(*types.Err)
		if !ok {
			return false, fmt.Errorf("cel evaluation failed: %v", val)
		}
		if isMissingAccess(e) {
			return true, error(e)
		}
		return false, error(e)
	}
	return false, nil
}

// checkContext maps an in-flight context error to a plain error so a cancelled
// evaluation is never mistaken for an empty stage or a nonmatch.
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("discovery evaluation cancelled: %w", err)
	}
	return nil
}
