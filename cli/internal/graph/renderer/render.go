package renderer

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

// Render renders the current state of the tree starting from the given root ID.
// If no root is provided, it attempts to determine the root from the graph.
// If no writer is provided, it defaults to os.Stdout. If no GraphRenderer is
// provided, it uses a default tree renderer that serializes the vertex ID.
//
// Render is a convenience function around GraphRenderer.Render providing a similar
// interface as StartRenderLoop, but without the live rendering loop.
func Render[T cmp.Ordered](
	ctx context.Context,
	dag *syncdag.DirectedAcyclicGraph[T],
	roots []T,
	writer io.Writer,
	renderer GraphRenderer[T],
) error {
	if renderer == nil {
		renderer = NewTreeRenderer(func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		})
		slog.InfoContext(ctx, "no graph renderer provided, using default tree renderer")
	}

	if writer == nil {
		writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}

	if err := renderer.Render(ctx, writer, dag, roots); err != nil {
		return fmt.Errorf("failed to render graph: %w", err)
	}
	return nil
}

// StartRenderLoop starts the rendering loop. It returns a function that can be
// used to wait for the rendering loop to complete.
// The rendering loop will run until the vertex with root id is in
// syncdag.TraversalState StateCompleted, an error occurs or the context is
// canceled.
// If no root is provided, it attempts to determine the root from the graph.
// If no writer is provided, it defaults to os.Stdout. If no GraphRenderer is
// provided, it uses a default tree renderer that serializes the vertex ID.
// If no refresh rate is provided, it defaults to 100ms for live rendering.
func StartRenderLoop[T cmp.Ordered](
	ctx context.Context,
	dag *syncdag.DirectedAcyclicGraph[T],
	roots []T,
	writer io.Writer,
	refreshRate time.Duration,
	renderer GraphRenderer[T],
) func() error {
	errCh := make(chan error)
	waitFunc := func() error {
		err := <-errCh
		return err
	}

	if renderer == nil {
		renderer = NewTreeRenderer(func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		})
		slog.InfoContext(ctx, "no graph renderer provided, using default tree renderer")
	}

	if refreshRate == 0 {
		refreshRate = 100 * time.Millisecond
		slog.InfoContext(ctx, "no refresh rate provided, using default 100ms for live rendering")
	}

	if writer == nil {
		writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}

	renderState := &renderLoopState{
		errCh:       errCh,
		refreshRate: refreshRate,
		writer:      writer,
		outputState: struct {
			displayedLines int
			lastOutput     string
		}{
			displayedLines: 0,
			lastOutput:     "",
		},
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Convert panic to error and send it
				select {
				case errCh <- fmt.Errorf("render loop panicked: %v", r):
				default: // Channel might be full, don't block
				}
			}
			close(errCh)
		}()
		renderLoop(ctx, dag, roots, renderer, renderState)
	}()
	return waitFunc
}

type renderLoopState struct {
	errCh       chan error
	refreshRate time.Duration
	writer      io.Writer
	outputState struct {
		displayedLines int
		lastOutput     string
	}
}

func renderLoop[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], roots []T, renderer GraphRenderer[T], renderState *renderLoopState) {
	ticker := time.NewTicker(renderState.refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			renderState.errCh <- ctx.Err()
			return
		case <-ticker.C:
			if err := refreshOutput(ctx, dag, roots, renderer, renderState); err != nil {
				renderState.errCh <- err
				return
			}
		}
	}
}

func refreshOutput[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], roots []T, renderer GraphRenderer[T], renderState *renderLoopState) error {
	outbuf := new(bytes.Buffer)
	if err := Render(ctx, dag, roots, outbuf, renderer); err != nil {
		return err
	}
	output := outbuf.String()

	// only update if the output has changed
	if output != renderState.outputState.lastOutput {
		// clear previous output
		var buf bytes.Buffer
		for range renderState.outputState.displayedLines {
			buf.WriteString(text.CursorUp.Sprint())
			buf.WriteString(text.EraseLine.Sprint())
		}
		if _, err := fmt.Fprint(renderState.writer, buf.String()); err != nil {
			return fmt.Errorf("error clearing previous output: %w", err)
		}

		if _, err := fmt.Fprint(renderState.writer, output); err != nil {
			return fmt.Errorf("error writing live rendering output to tree display manager writer: %w", err)
		}
		renderState.outputState.lastOutput = output
		renderState.outputState.displayedLines = strings.Count(output, "\n")
	}
	return nil
}
