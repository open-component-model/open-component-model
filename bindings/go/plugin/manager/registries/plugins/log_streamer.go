package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// StartLogStreamer is intended to be launched when the plugin is started. It will continuously stream logs
// from the plugin to the context debug logger. Logs in the plugin has to be written to stderr.
// The expected logger using stderr should be set up like this:
//
//	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
//			Level: slog.LevelDebug, // debug level here is respected when sending this message.
//	}))
func StartLogStreamer(ctx context.Context, plugin *types.Plugin) {
	if plugin.Stderr == nil {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create a scanner to read output line by line
	scanner := bufio.NewScanner(plugin.Stderr)

	lineChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start a goroutine to scan for the port number
	go func() {
		defer close(lineChan)
		defer close(errChan)

		for scanner.Scan() {
			select {
			case lineChan <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case <-ctx.Done():
				return
			case errChan <- fmt.Errorf("error reading plugin output: %w", err):
				// Error sent successfully
			}
		}
	}()

	// start streaming log messages to the debug context log
	for {
		select {
		case line := <-lineChan:
			parsed, err := parseLine(line)
			if err != nil {
				// we don't log this one, otherwise the output gets very crowded during shutdown
				// until this realises that it should stop parsing.
				continue
			}

			var log func(ctx context.Context, msg string, args ...any)

			switch parsed.level {
			case slog.LevelDebug.String():
				log = slog.DebugContext
			case slog.LevelInfo.String():
				log = slog.InfoContext
			case slog.LevelWarn.String():
				log = slog.WarnContext
			case slog.LevelError.String():
				log = slog.ErrorContext
			default:
				slog.DebugContext(ctx, "unknown log level", "level", parsed.level)
				continue
			}

			log(ctx, parsed.msg, parsed.args...)
		case err, ok := <-errChan:
			if ok {
				slog.ErrorContext(ctx, "streaming logs from plugin failed", "error", err)
			}
		case <-ctx.Done():
			// context is done, we stop streaming logs
			slog.DebugContext(ctx, "stopping log streamer, context is done")
			return
		}
	}
}

// record represents a single log line
type record struct {
	msg   string
	level string
	args  []any
}

func parseLine(line string) (record, error) {
	parsed := map[string]any{}
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		return record{}, err
	}
	if _, ok := parsed["msg"]; !ok {
		return record{}, fmt.Errorf("no 'msg' field in line %q", line)
	}
	if _, ok := parsed["level"]; !ok {
		return record{}, fmt.Errorf("no 'level' field in line %q", line)
	}

	var result record
	result.msg = parsed["msg"].(string)
	result.level = parsed["level"].(string)
	delete(parsed, "msg")
	delete(parsed, "level")

	var keys []string

	for k := range parsed {
		keys = append(keys, k)
	}

	// make the output determinable
	sort.Strings(keys)

	for _, k := range keys {
		result.args = append(result.args, k, parsed[k])
	}

	return result, nil
}
