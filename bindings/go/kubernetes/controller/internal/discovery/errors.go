package discovery

import (
	"context"
	"errors"
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

// missingAccessPrefixes are the messages cel-go's attribute resolution emits
// for a failed lookup. All three come from the same unexported
// *resolutionError (interpreter/attributes.go), which v0.31 does not export,
// so detection relies on the message prefix. Genuine CEL errors such as
// "no such overload" must never match. Pinned by TestIsMissingAccessShapes,
// which drives each shape through a real evaluation.
var missingAccessPrefixes = []string{
	"no such key:",
	"no such attribute",
	"index out of bounds:",
}

// isMissingAccess reports whether err is a CEL attribute-resolution failure,
// i.e. access to a missing map key, a missing attribute, or an out-of-range
// list index.
func isMissingAccess(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, prefix := range missingAccessPrefixes {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// evalResult classifies the result of a CEL evaluation. Missing attribute or
// key access is reported as missing, with the original error retained as
// cause for callers that treat missing access as an error. All other CEL
// errors are reported as failures.
//
// ctx is re-checked first because cel-go reports cancellation as an ordinary
// evaluation error ("operation interrupted"), which matches no missing-access
// prefix and would otherwise be classified as a semantic failure. Callers wrap
// failures in *SelectorError or *ExtractError, which the controller treats as
// terminal, so a cancelled reconcile would stall the object permanently. Every
// ContextEval in this package goes through here so it's enough to do a checkContext
// in this function.
func evalResult(ctx context.Context, val ref.Val, err error) (missing bool, cause error) {
	if ctxErr := checkContext(ctx); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil {
		return isMissingAccess(err), err
	}
	if types.IsError(val) {
		// Note if your IDE flags this:
		// `types.IsError` is itself a `switch val.(type) { case *Err: }`, so inside this
		// branch the dynamic type is exactly *types.Err and the assertion cannot fail.
		// errors.As is not an option: ref.Val has no Error method.
		celErr := val.(*types.Err)

		return isMissingAccess(celErr), error(celErr)
	}

	return false, nil
}

// errCancelled marks a context failure observed during evaluation. It is never
// wrapped in a *SelectorError or *ExtractError, so cancellation stays
// retryable instead of stalling the object.
var errCancelled = errors.New("discovery evaluation cancelled")

// checkContext maps an in-flight context error to a plain error so a cancelled
// evaluation is never mistaken for an empty stage or a nonmatch.
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", errCancelled, err)
	}

	return nil
}

// selectorEvalError wraps a selector evaluation failure for a stage. A
// cancellation passes through untouched: only this constructor and
// extractEvalError are used on evaluation paths, so it is not possible to
// wrap a cancelled evaluation in a terminal error by forgetting a check.
func selectorEvalError(stage string, cause error) error {
	if errors.Is(cause, errCancelled) {
		return cause
	}

	return &SelectorError{Stage: stage, Cause: fmt.Errorf("failed to evaluate selector expression: %w", cause)}
}

// extractEvalError wraps an extraction evaluation failure for a field, passing
// a cancellation through untouched. See selectorEvalError.
func extractEvalError(field string, cause error) error {
	if errors.Is(cause, errCancelled) {
		return cause
	}

	return &ExtractError{Field: field, Cause: fmt.Errorf("failed to evaluate extract expression: %w", cause)}
}
