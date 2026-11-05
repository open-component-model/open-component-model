package logtest_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/logtest"
)

func TestCapture_MinimumLevel(t *testing.T) {
	tests := []struct {
		name     string
		min      slog.Level
		wantMsgs []string
	}{
		{name: "debug keeps everything", min: slog.LevelDebug, wantMsgs: []string{"debug", "info", "warn"}},
		{name: "warn drops lower levels", min: slog.LevelWarn, wantMsgs: []string{"warn"}},
		{name: "error records nothing", min: slog.LevelError, wantMsgs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			capture := logtest.New(tt.min)
			logger := slog.New(capture)

			logger.Debug("debug")
			logger.Info("info")
			logger.Warn("warn")

			r.Equal(tt.wantMsgs, capture.Messages())
			r.Len(capture.Records(), len(tt.wantMsgs))
		})
	}
}

func TestCapture_Default(t *testing.T) {
	r := require.New(t)
	prev := slog.Default()

	t.Run("captures the default logger", func(t *testing.T) {
		r := require.New(t)
		capture := logtest.Default(t, slog.LevelInfo)
		r.NotSame(prev, slog.Default())

		slog.Info("hello", "key", "value")
		slog.Debug("dropped")

		r.Equal([]string{"hello"}, capture.Messages())
		r.Equal(map[string]any{"key": "value"}, logtest.Attrs(capture.Records()[0]))
	})

	r.Same(prev, slog.Default(), "the previous default logger must be restored")
}

func TestCapture_Contains(t *testing.T) {
	tests := []struct {
		name string
		log  func(*slog.Logger)
		want bool
	}{
		{name: "in the message", log: func(l *slog.Logger) { l.Info("token secret-token leaked") }, want: true},
		{name: "in an attribute", log: func(l *slog.Logger) { l.Info("signing", "token", "secret-token") }, want: true},
		{name: "in a group attribute", log: func(l *slog.Logger) { l.Info("signing", slog.Group("creds", "token", "secret-token")) }, want: true},
		{name: "in a With attribute", log: func(l *slog.Logger) { l.With("token", "secret-token").Info("signing") }, want: true},
		{name: "in a With attribute under a group", log: func(l *slog.Logger) { l.WithGroup("creds").With("token", "secret-token").Info("signing") }, want: true},
		{name: "absent", log: func(l *slog.Logger) { l.Info("signing", "token", "[redacted]") }, want: false},
		{name: "below the minimum level", log: func(l *slog.Logger) { l.Debug("secret-token") }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			capture := logtest.New(slog.LevelInfo)
			tt.log(slog.New(capture))
			r.Equal(tt.want, capture.Contains("secret-token"))
		})
	}
}

func TestCapture_WithAttrsAndGroups(t *testing.T) {
	r := require.New(t)
	capture := logtest.New(slog.LevelDebug)
	logger := slog.New(capture)

	logger.With("a", "1").WithGroup("g").With("b", "2").Info("msg", "c", "3")
	logger.WithGroup("empty").Info("plain")

	records := capture.Records()
	r.Len(records, 2, "derived loggers must write to the same capture")
	r.Equal(map[string]any{
		"a": "1",
		"g": []slog.Attr{slog.String("b", "2"), slog.String("c", "3")},
	}, logtest.Attrs(records[0]))
	r.Empty(logtest.Attrs(records[1]), "a group with no attributes is dropped")
}

func TestCapture_Reset(t *testing.T) {
	r := require.New(t)
	capture := logtest.New(slog.LevelDebug)
	logger := slog.New(capture)

	logger.Info("before")
	capture.Reset()
	r.Nil(capture.Messages())

	logger.Info("after")
	r.Equal([]string{"after"}, capture.Messages())
}

func TestCapture_RecordsIsACopy(t *testing.T) {
	r := require.New(t)
	capture := logtest.New(slog.LevelDebug)
	slog.New(capture).Info("one")

	records := capture.Records()
	records[0].Message = "changed"

	r.Equal([]string{"one"}, capture.Messages())
}
