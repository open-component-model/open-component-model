// Package logtest captures [log/slog] records in tests.
//
// A test that checks what a package logs installs a [Capture] as the default logger:
//
//	logs := logtest.Default(t, slog.LevelDebug)
//	doSomething(t.Context())
//	r.Contains(logs.Messages(), "something happened")
//	r.False(logs.Contains(secret)) // also sees attributes added with Logger.With
//
// [Default] swaps the process-wide default logger, so a test that calls it must not run in
// parallel with tests that log. A test that owns its logger uses [New] instead and reads the
// attributes of a record with [Attrs]:
//
//	capture := logtest.New(slog.LevelInfo)
//	logger := slog.New(capture)
//	attrs := logtest.Attrs(capture.Records()[0]) // map[string]any
package logtest

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// Capture is a [slog.Handler] that records every record at or above a minimum level. It is
// safe for concurrent use. Attributes and groups added with [slog.Logger.With] or
// [slog.Logger.WithGroup] are kept: the derived handler writes to the same store and adds
// them to each record, so a secret attached with With still shows up in [Capture.Contains].
type Capture struct {
	min   slog.Level
	store *store
	// scopes holds the With attributes and WithGroup names in the order they were added.
	scopes []scope
}

// store is the record list that a Capture shares with the handlers derived from it.
type store struct {
	mu      sync.Mutex
	records []slog.Record
}

// scope is one WithAttrs or WithGroup call: a group name, or a list of attributes.
type scope struct {
	group string
	attrs []slog.Attr
}

// New returns a Capture that records every record at or above level.
func New(level slog.Level) *Capture {
	return &Capture{min: level, store: &store{}}
}

// Default installs a new Capture as the default logger for the rest of the test and restores
// the previous logger when the test ends. See the package comment about parallel tests.
func Default(t *testing.T, level slog.Level) *Capture {
	t.Helper()
	capture := New(level)
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return capture
}

// Enabled implements [slog.Handler].
func (c *Capture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= c.min
}

// Handle implements [slog.Handler].
func (c *Capture) Handle(_ context.Context, record slog.Record) error {
	if len(c.scopes) > 0 {
		record = c.scoped(record)
	} else {
		record = record.Clone()
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.records = append(c.store.records, record)
	return nil
}

// scoped returns a copy of record with the With attributes added and its attributes nested
// in the WithGroup groups, the way a text or JSON handler would print them.
func (c *Capture) scoped(record slog.Record) slog.Record {
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	for i := len(c.scopes) - 1; i >= 0; i-- {
		s := c.scopes[i]
		if s.group == "" {
			attrs = append(append([]slog.Attr{}, s.attrs...), attrs...)
			continue
		}
		if len(attrs) > 0 {
			attrs = []slog.Attr{{Key: s.group, Value: slog.GroupValue(attrs...)}}
		}
	}
	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	out.AddAttrs(attrs...)
	return out
}

// WithAttrs implements [slog.Handler]. The returned handler shares the record store.
func (c *Capture) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return c
	}
	return c.with(scope{attrs: attrs})
}

// WithGroup implements [slog.Handler]. The returned handler shares the record store.
func (c *Capture) WithGroup(name string) slog.Handler {
	if name == "" {
		return c
	}
	return c.with(scope{group: name})
}

func (c *Capture) with(s scope) *Capture {
	scopes := make([]scope, 0, len(c.scopes)+1)
	scopes = append(append(scopes, c.scopes...), s)
	return &Capture{min: c.min, store: c.store, scopes: scopes}
}

// Records returns a copy of the recorded records in the order they were logged.
func (c *Capture) Records() []slog.Record {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	out := make([]slog.Record, len(c.store.records))
	copy(out, c.store.records)
	return out
}

// Messages returns the messages of the recorded records in the order they were logged, or nil
// when nothing was recorded.
func (c *Capture) Messages() []string {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	var out []string
	for _, record := range c.store.records {
		out = append(out, record.Message)
	}
	return out
}

// Reset drops the recorded records.
func (c *Capture) Reset() {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.records = nil
}

// Contains reports whether the message or an attribute value of any recorded record contains
// s. Use it to check that a secret never reaches the log.
func (c *Capture) Contains(s string) bool {
	for _, record := range c.Records() {
		if strings.Contains(record.Message, s) {
			return true
		}
		found := false
		record.Attrs(func(attr slog.Attr) bool {
			found = strings.Contains(attr.Value.String(), s)
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// Attrs returns the attributes of record keyed by name. Groups are not flattened.
func Attrs(record slog.Record) map[string]any {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	return attrs
}
