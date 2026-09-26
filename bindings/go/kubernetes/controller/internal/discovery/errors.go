package discovery

import (
	"context"
	"fmt"
	"strings"

	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// Selector stages. They name the stage of a SelectorError and the stage that
// emptied a Filtered result, and are rendered verbatim into the Ready message
// of the Discovery, so each must read as a singular noun.
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

// selectorErrorf builds a *SelectorError. Callers pass the cause with %w so
// errors.Is and errors.As keep working through the wrapper.
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

// extractErrorf builds an *ExtractError. Callers pass the cause with %w so
// errors.Is and errors.As keep working through the wrapper.
func extractErrorf(field, format string, args ...any) *ExtractError {
	return &ExtractError{Field: field, Cause: fmt.Errorf(format, args...)}
}

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

// isMissingAccess we use this if the element in question doesn't have
// the field CEL is trying to assert. That shouldn't be a hard error.
// That should simply mean that the element in question does not match
// our selector. And that's it. That's the reason for the above
// missingAccessPrefix's existence. We need to assert those prefixes
// to figure out that our check was incorrect OR that the item in question
// simply doesn't match. For example, given a CEL expression that checks
// whether platform equals `linux` should NOT FAIL on an element that
// doesn't have a platform field.
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
// ctx is re-checked first so a cancellation is caught even when cel-go returns
// a successful value just as the context dies: without it the pipeline would
// keep working against a dead context until the next loop head.
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

// checkContext maps an in-flight context error to an error so a cancelled
// evaluation is never mistaken for an empty stage or a nonmatch.
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("discovery evaluation cancelled: %w", err)
	}

	return nil
}
