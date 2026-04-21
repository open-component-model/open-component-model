# progress

Progress rendering system for long-running CLI operations.

## Overview

The package separates flow control (`Tracker`, `Operation`) from visualization (`Visualizer[T]`),
allowing different rendering backends (terminal progress bar, plain logging) to be plugged in per operation.

## Key Types

**`Tracker[T]`** is the entry point. It manages slog redirection during progress rendering
(buffering log output so it doesn't corrupt the terminal UI) and creates operations
via `StartOperation`.

**`Operation`** represents a running unit of work. It is created by `StartOperation` and
exposes `Finish(err)` — pass `nil` for success or an error for failure. The operation
drives the visualizer lifecycle internally — `Begin` on start, `End` on finish.

**`Visualizer[T]`** is the rendering interface with three methods: `Begin(name)`,
`HandleEvent(Event[T])`, and `End(err)`.

## Simple Operation

```go
tracker := progress.NewTracker(ctx, os.Stderr, bar.NewVisualizer[myType])
defer tracker.Stop()

op := tracker.StartOperation("Resolving components")
result, err := doWork()
op.Finish(err) // renders ✓ on success, ✗ + error on failure
```

## Tracked Operation

```go
op := tracker.StartOperation("Transferring",
    progress.WithEvents(graph.Events(), mapEvent, graph.NodeCount()),
    progress.WithErrorFormatter(formatError))

err := graph.Process(ctx)
op.Finish(err)
```

## Non-Terminal Mode

When the output is not a terminal (e.g. piped to a file or CI), the tracker
detects this automatically:

- A slog-based visualizer logs operation start/finish via the default logger
- Events (if configured via `WithEvents`) are logged via slog
- slog output is not intercepted — logs flow to their original destination

## Visualizer Implementations

The `bar` subpackage provides an ANSI terminal visualizer:

- `NewVisualizer[T]` — for simple operations (total=0) shows an animated spinner header;
  for tracked operations shows a progress bar with scrolling item log

## Package Layout

```text
progress/
  README.md               this file
  doc.go                  Go package documentation
  tracker.go              Tracker[T], Operation, WithEvents, terminal detection
  visualizer.go           Visualizer[T], VisualizerFactory[T], ErrorFormatterSetter[T]
  slog.go                 slog buffering (SyncBuffer, LogBufferAware)
  slog_visualizer.go      SlogVisualizer[T] for non-terminal mode
  bar/                    ANSI terminal visualizer implementation
    doc.go                package documentation
    style.go              ANSI escape codes and theme constants
    animation.go          spinner, shimmer, animation loop
    bar_visualizer.go     progress visualizer (spinner and progress bar)
    format.go             error tree, framed text, sidebar formatting
```
