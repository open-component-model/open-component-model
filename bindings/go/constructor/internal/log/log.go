package log

import (
	"log/slog"
)

// Base returns a base logger for constructor operations with a default RealmKey.
func Base() *slog.Logger {
	return slog.With(slog.String("realm", "constructor"))
}
