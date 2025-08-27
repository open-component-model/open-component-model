package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	// StaticRenderMode corresponds to a one-time call to RenderOnce.
	StaticRenderMode = "static"
	// LiveRenderMode corresponds to RunRenderLoop.
	LiveRenderMode = "live"
)

// Renderer defines an interface for rendering arbitrary data structures.
type Renderer interface {
	Render(ctx context.Context, writer io.Writer) error
}

var DefaultRefreshRate = 100 * time.Millisecond

// RenderOnce calls the Renderer.Render function once.
// If no writer is provided, it defaults to os.Stdout.
//
// RenderOnce is a convenience function around Renderer.Render providing a similar
// interface as RunRenderLoop, but without the live rendering loop.
func RenderOnce(ctx context.Context, renderer Renderer, opts ...RenderOption) error {
	options := &RenderOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if renderer == nil {
		return fmt.Errorf("renderer cannot be nil")
	}

	if options.Writer == nil {
		options.Writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}

	if err := renderer.Render(ctx, options.Writer); err != nil {
		return fmt.Errorf("failed to render graph: %w", err)
	}
	return nil
}

// RunRenderLoop starts the rendering loop. It returns a function that can be
// used to wait for the rendering loop to return.
// The rendering loop will run until the context is cancelled or an error
// occurs.
// If no writer is provided, it defaults to os.Stdout.
// If no refresh rate is provided, it defaults to DefaultRefreshRate.
// TODO(fabianburth): Implement render loop in a TUI. Currently, the feature
//
//	is experimental, as the ASCI control sequences used to reset the output
//	only work as long as the output fits into the terminal window (you may
//	scroll very far out though ;) ).
//	https://github.com/open-component-model/ocm-project/issues/608
func RunRenderLoop(ctx context.Context, renderer Renderer, opts ...RenderLoopOption) func() error {
	options := &RenderLoopOptions{}

	for _, opt := range opts {
		opt(options)
	}

	if options.RefreshRate == 0 {
		options.RefreshRate = DefaultRefreshRate
		slog.InfoContext(ctx, "no refresh rate provided, using default 100ms for live rendering")
	}

	if options.Writer == nil {
		options.Writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}

	if renderer == nil {
		return func() error {
			return fmt.Errorf("renderer cannot be nil")
		}
	}

	renderState := &renderLoopState{
		refreshRate: options.RefreshRate,
		writer:      options.Writer,
		outputState: struct {
			displayedLines int
			lastOutput     string
		}{
			displayedLines: 0,
			lastOutput:     "",
		},
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = errors.Join(err, fmt.Errorf("render loop panicked: %v", r))
			}
		}()
		return renderLoop(ctx, renderer, renderState)
	})

	return eg.Wait
}

type renderLoopState struct {
	refreshRate time.Duration
	writer      io.Writer
	outputState struct {
		displayedLines int
		lastOutput     string
	}
}

func renderLoop(ctx context.Context, renderer Renderer, renderState *renderLoopState) error {
	ticker := time.NewTicker(renderState.refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := refreshOutput(context.Background(), renderer, renderState); err != nil {
				return err
			}
			return ctx.Err()
		case <-ticker.C:
			if err := refreshOutput(ctx, renderer, renderState); err != nil {
				return err
			}
		}
	}
}

func refreshOutput(ctx context.Context, renderer Renderer, renderState *renderLoopState) error {
	outbuf := new(bytes.Buffer)
	if err := RenderOnce(ctx, renderer, WithWriter(outbuf)); err != nil {
		return err
	}
	output := outbuf.String()

	// only update if the output has changed
	if output != renderState.outputState.lastOutput {
		// clear previous output
		if _, err := fmt.Fprint(renderState.writer, EraseNLines(renderState.outputState.displayedLines)); err != nil {
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
