package component_version

import (
	"encoding/json"
	"fmt"

	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/cli/internal/render/progress"
	"ocm.software/open-component-model/cli/internal/render/progress/bar"
)

// mapEvent converts a graph runtime progress event to a typed progress.Event.
func mapEvent(e graphRuntime.ProgressEvent) progress.Event[*graphPkg.Transformation] {
	return progress.Event[*graphPkg.Transformation]{
		ID:    e.Transformation.ID,
		Name:  formatTransformationName(e.Transformation),
		State: mapState(e.State),
		Err:   e.Err,
		Data:  e.Transformation,
	}
}

// mapState converts a graph runtime state to a progress state.
func mapState(s graphRuntime.State) progress.State {
	switch s {
	case graphRuntime.Running:
		return progress.Running
	case graphRuntime.Completed:
		return progress.Completed
	case graphRuntime.Failed:
		return progress.Failed
	default:
		return progress.Unknown
	}
}

// formatError renders a transformation error with a red sidebar error tree
// and a framed spec dump for debugging.
func formatError(t *graphPkg.Transformation, err error) string {
	result := bar.SidebarText("", bar.TreeErrorFormatter(err), bar.Red)

	if t != nil && t.Spec != nil {
		info := fmt.Sprintf("Transformation %q of type %s/%s failed.\nSpec data shown below for debugging.",
			t.ID, t.Type.Name, t.Type.Version)
		specJSON, jsonErr := json.MarshalIndent(t.Spec.Data, "", "  ")
		if jsonErr == nil {
			result += bar.FramedText(info, string(specJSON), 4)
		}
	}

	return result
}

// formatTransformationName returns a display name as "ID [Type]".
func formatTransformationName(t *graphPkg.Transformation) string {
	return fmt.Sprintf("%s [%s]", t.ID, t.Type.Name)
}
