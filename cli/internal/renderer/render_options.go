package renderer

import (
	"io"
	"time"
)

// RenderLoopOptions defines the options for the RunRenderLoop.
type RenderLoopOptions struct {
	// RefreshRate is the rate at which the renderer should refresh the output.
	RefreshRate time.Duration
	RenderOptions
}

// RenderLoopOption is a function that modifies the RenderLoopOptions.
// RenderLoopOption can be passed to RunRenderLoop to customize the rendering
// behavior.
type RenderLoopOption func(*RenderLoopOptions)

// WithRefreshRate returns a RenderLoopOption that sets the refresh rate for
// the rendering loop. The refresh rate determines how often the render loop
// will refresh the output.
func WithRefreshRate(refreshRate time.Duration) RenderLoopOption {
	return func(opts *RenderLoopOptions) {
		opts.RefreshRate = refreshRate
	}
}

// WithRenderOptions returns a RenderLoopOption that sets the RenderOptions on
// the RenderLoopOptions.
func WithRenderOptions(opts ...RenderOption) RenderLoopOption {
	return func(options *RenderLoopOptions) {
		for _, opt := range opts {
			opt(&options.RenderOptions)
		}
	}
}

// RenderOptions defines the options for RenderOnce.
type RenderOptions struct {
	// Writer is the writer to which the renderer should write the output.
	// Typically, this is os.Stdout.
	Writer io.Writer
}

// RenderOption is a function that modifies the RenderOptions.
// RenderOption can be passed to RenderOnce to customize the rendering
// behavior.
type RenderOption func(*RenderOptions)

// WithWriter returns a RenderOption that sets the writer for the renderer.
func WithWriter(writer io.Writer) RenderOption {
	return func(opts *RenderOptions) {
		opts.Writer = writer
	}
}
