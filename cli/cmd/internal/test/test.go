// Package test provides utilities for testing the OCM CLI commands.
// It includes helpers for executing commands and parsing JSON log output.
package test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

// Options holds configuration for executing OCM CLI commands in tests
type Options struct {
	args   []string  // Command line arguments to pass to the CLI
	out    io.Writer // Output writer to capture command output
	errout io.Writer // Error writer to capture command errors and logs
	format string    // Log format to use (e.g., json, text)
}

// Option is a function that configures Options
type Option func(*Options)

// WithArgs sets the command line arguments for the OCM CLI command
func WithArgs(args ...string) Option {
	return func(o *Options) {
		o.args = args
	}
}

// WithOutput sets the output writer to capture command output
func WithOutput(out io.Writer) Option {
	return func(o *Options) {
		o.out = out
	}
}

func WithErrorOutput(errout io.Writer) Option {
	return func(o *Options) {
		o.errout = errout
	}
}

// WithLogFormat sets the log format for the OCM CLI command
func WithLogFormat(format string) Option {
	return func(o *Options) {
		o.format = format
	}
}

// OCM executes an OCM CLI command with the given options and returns the command and any error
// It's designed to be used in tests to run OCM commands and capture their output
func OCM(tb testing.TB, opts ...Option) (*cobra.Command, error) {
	tb.Helper()

	opt := Options{}
	for _, o := range opts {
		o(&opt)
	}
	tb.Helper()
	instance := cmd.New()
	if len(opt.args) == 0 {
		opt.args = []string{"help"}
	}

	// if and output is set, mirror it towards stdout (for logging) and the given output for testing
	if opt.out != nil {
		instance.SetOut(io.MultiWriter(os.Stdout, opt.out))
	}
	if opt.errout != nil {
		instance.SetErr(io.MultiWriter(os.Stderr, opt.errout))
	}

	// by default lets test with the json format so its actually easier to read and test against
	if opt.format == "" {
		opt.format = log.FormatJSON
	}
	f := instance.PersistentFlags().Lookup(log.FormatFlagName)
	if err := f.Value.Set(opt.format); err != nil {
		return nil, fmt.Errorf("failed to set format: %w", err)
	}

	// ensure verbose logs during tests for better diagnostics
	lf := instance.PersistentFlags().Lookup(log.LevelFlagName)
	if lf != nil {
		if err := lf.Value.Set(log.LevelDebug); err != nil {
			return nil, fmt.Errorf("failed to set log level: %w", err)
		}
	}

	instance.SetArgs(opt.args)
	return instance.ExecuteContextC(tb.Context())
}

// JSONLogReader provides functionality to read and parse JSON-formatted log output
// It maintains both the main log buffer and a buffer for discarded (non-JSON) entries
type JSONLogReader struct {
	*bytes.Buffer
	Discarded *bytes.Buffer
}

// NewJSONLogReader creates a new JSONLogReader with initialized buffers
func NewJSONLogReader() *JSONLogReader {
	return &JSONLogReader{
		Buffer:    bytes.NewBuffer(make([]byte, 0, 1024)),
		Discarded: bytes.NewBuffer(make([]byte, 0, 1024)),
	}
}

// JSONLogEntry represents a single log entry in JSON format
type JSONLogEntry struct {
	Time  string `json:"time"`  // Timestamp of the log entry
	Level string `json:"level"` // Log level (e.g., info, error)
	Msg   string `json:"msg"`   // Log message

	// Additional dynamic fields can be unmarshaled into a map
	Extras map[string]interface{} `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling for JSONLogEntry
// It handles both standard fields and additional dynamic fields
func (l *JSONLogEntry) UnmarshalJSON(data []byte) error {
	// Unmarshal into a temporary map
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract known fields
	if v, ok := raw["time"].(string); ok {
		l.Time = v
	}
	if v, ok := raw["level"].(string); ok {
		l.Level = v
	}
	if v, ok := raw["msg"].(string); ok {
		l.Msg = v
	}

	// Remove known fields and assign the rest to Extras
	delete(raw, "time")
	delete(raw, "level")
	delete(raw, "msg")
	l.Extras = raw

	return nil
}

// List parses the log buffer and returns all valid JSON log entries
// Non-JSON entries are written to the Discarded buffer
func (logs *JSONLogReader) List() ([]*JSONLogEntry, error) {
	scanner := bufio.NewScanner(logs.Buffer)
	var entries []*JSONLogEntry
	for scanner.Scan() {
		data := scanner.Bytes()
		entry := JSONLogEntry{}
		if err := json.Unmarshal(data, &entry); err == nil {
			entries = append(entries, &entry)
		} else if _, err := logs.Discarded.Write(append(data, []byte{'\n'}...)); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// GetDiscarded returns the contents of the Discarded buffer as a string
// This contains any non-JSON log entries that were encountered
func (logs *JSONLogReader) GetDiscarded() string {
	return logs.Discarded.String()
}
