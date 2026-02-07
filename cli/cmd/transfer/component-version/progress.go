package component_version

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/cli/internal/render/progress"
	"ocm.software/open-component-model/cli/internal/render/progress/bar"
	"ocm.software/open-component-model/cli/internal/render/progress/simple"
)

// newProgressTracker creates a progress tracker that visualizes graph execution progress.
// It maps graph runtime events to progress states and formats transformations for display.
func newProgressTracker(graph *builder.Graph, out io.Writer) *progress.Tracker[*graphPkg.Transformation, graphRuntime.ProgressEvent] {
	visualiser := selectVisualizer(out)

	return progress.NewTracker(
		progress.WithEvents(graph.Events(), mapEvent),
		progress.WithOutput[*graphPkg.Transformation, graphRuntime.ProgressEvent](out),
		progress.WithTotal[*graphPkg.Transformation, graphRuntime.ProgressEvent](graph.NodeCount()),
		progress.WithVisualizer[*graphPkg.Transformation, graphRuntime.ProgressEvent](visualiser),
	)
}

// mapEvent converts a graph runtime progress event to a progress event.
func mapEvent(e graphRuntime.ProgressEvent) progress.Event[*graphPkg.Transformation] {
	// mapState converts graph runtime state to progress state.
	mapState := func(s graphRuntime.State) progress.State {
		switch s {
		case graphRuntime.Running:
			return progress.Running
		case graphRuntime.Completed:
			return progress.Completed
		case graphRuntime.Failed:
			return progress.Failed
		default:
			return ""
		}
	}
	return progress.Event[*graphPkg.Transformation]{
		ID:    e.Transformation.ID,
		Data:  e.Transformation,
		State: mapState(e.State),
		Err:   e.Err,
	}
}

// selectVisualizer returns the bar visualizer when the output is a terminal,
// otherwise falls back to the simple text-based visualizer.
func selectVisualizer(out io.Writer) progress.VisualizerFactory[*graphPkg.Transformation] {
	if progress.IsTerminal(out) {
		return bar.NewBarVisualizer(
			bar.WithHeader[*graphPkg.Transformation]("Transferring component versions..."),
			bar.WithNameFormatter(func(t *graphPkg.Transformation) string {
				return fmt.Sprintf("%s [%s]", t.ID, t.Type.Name)
			}),
			bar.WithErrorFormatter(func(t *graphPkg.Transformation, err error) string {
				info := fmt.Sprintf("Transformation %q of type %s/%s failed. \nSpec data shown below for debugging.",
					t.ID, t.Type.Name, t.Type.Version)
				specJSON, _ := json.MarshalIndent(t.Spec.Data, "", "  ")
				return fmt.Sprintf("%s\n%s", bar.TreeErrorFormatter(err), bar.FramedText(info, string(specJSON), 4))
			}),
		)
	}
	return simple.NewSimpleVisualizer[*graphPkg.Transformation](slog.New(slog.NewTextHandler(out, nil)))
}
