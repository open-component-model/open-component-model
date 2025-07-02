// Package filter provides functionality for filtering log records based on custom criteria.
// It implements a slog.Handler wrapper that can filter log messages based on attribute values
// and minimum log levels, commonly used for realm-based logging control.
package filter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	v1 "ocm.software/open-component-model/cli/internal/flags/log/config/v1"
)

// LoggingKeyRealm is the default key used to identify the realm attribute in log records.
// This constant defines the standard key name for realm-based filtering.
const LoggingKeyRealm = "realm"

// filter wraps a slog.Handler and applies filtering logic based on attribute values.
// It filters out log records that don't meet the minimum level requirements for their realm.
type filter struct {
	handler slog.Handler          // The underlying handler to delegate to
	filters map[string]slog.Level // Map of realm names to minimum log levels
	key     string                // The attribute key to use for filtering (e.g., "realm")
	preset  string                // Preset value for the key, if any
}

func (f *filter) Enabled(ctx context.Context, level slog.Level) bool {
	return f.handler.Enabled(ctx, level)
}

func (f *filter) WithAttrs(attrs []slog.Attr) slog.Handler {
	preset := f.preset
	if preset == "" {
		for _, attr := range attrs {
			if attr.Key == f.key {
				preset = attr.Value.String()
				break
			}
		}
	}
	return &filter{
		handler: f.handler.WithAttrs(attrs),
		filters: f.filters,
		key:     f.key,
		preset:  preset,
	}
}

func (f *filter) WithGroup(name string) slog.Handler {
	return &filter{
		handler: f.handler.WithGroup(name),
		filters: f.filters,
		key:     f.key,
	}
}

// New creates a new filtered log handler that wraps the provided handler.
// The returned handler will filter log records based on the specified key and level filters.
//
// Parameters:
//   - handler: The underlying slog.Handler to delegate filtered records to
//   - key: The attribute key to use for filtering (e.g., LoggingKeyRealm)
//   - filters: Map of attribute values to minimum log levels
//
// Returns a slog.Handler that applies the specified filtering rules.
func New(handler slog.Handler, key string, filters map[string]slog.Level) slog.Handler {
	return &filter{
		handler: handler,
		filters: filters,
		key:     key,
	}
}

func GetRealmFiltersFromConfig(cfg *v1.Config) (map[string]slog.Level, error) {
	realmFilters := make(map[string]slog.Level)

	for _, rule := range cfg.Settings.Rules {
		var level slog.Level
		if err := level.UnmarshalText([]byte(rule.Level)); err != nil {
			return nil, fmt.Errorf("invalid log level in filter %s: %w", rule.Level, err)
		}
		for _, condition := range rule.Conditions {
			if condition.Realm == "" {
				return nil, fmt.Errorf("condition realm cannot be empty in rule: %v", rule)
			}
			realmFilters[condition.Realm] = level
		}
	}

	return realmFilters, nil
}

func NewFromV1Config(base slog.Handler, cfg v1.Config) (slog.Handler, error) {
	realmFilters, err := GetRealmFiltersFromConfig(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get realm filters from config: %w", err)
	}
	if len(realmFilters) > 0 {
		base = New(base, LoggingKeyRealm, realmFilters)
	}
	return base, nil
}

// Handle processes a log record by applying filtering logic before delegating to the underlying handler.
// If the record should be filtered out, it returns nil without processing the record.
// Otherwise, it delegates the record to the wrapped handler.
func (f *filter) Handle(ctx context.Context, record slog.Record) error {
	if f.shouldFilter(record) {
		return nil
	}
	return f.handler.Handle(ctx, record)
}

// shouldFilter determines whether a log record should be filtered out based on its realm and level.
// A record is filtered if:
//   - It has a realm attribute that exists in the filters map
//   - The record's level is below the minimum level specified for that realm
//
// Returns true if the record should be filtered out, false otherwise.
func (f *filter) shouldFilter(record slog.Record) bool {
	keyValue := f.preset
	if keyValue == "" {
		keyValue = f.getValueFromRecord(record)
	}
	if keyValue == "" {
		return false // No keyValue specified, don't filter
	}

	minLevel, exists := f.filters[keyValue]
	return exists && record.Level < minLevel
}

// getValueFromRecord extracts the value based on key from a log record's attributes.
// It searches through the record's attributes for the configured key and returns its string value.
// If the key is not found, it returns an empty string.
func (f *filter) getValueFromRecord(record slog.Record) string {
	var value string
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == f.key {
			value = attr.Value.String()
			return false // Stop iteration once we find the key
		}
		return true // Continue iteration
	})
	return value
}

// KeyFiltersFromStrings parses filter specifications from string arguments.
// Each filter string should be in the format "key=level" where:
//   - key is the attribute value to filter on (e.g., realm name)
//   - level is a valid slog.Level string (e.g., "info", "warn", "error")
//
// Example usage:
//
//	filters, err := KeyFiltersFromStrings("database=warn", "api=info")
//
// Returns a map of keys to log levels and any parsing error encountered.
func KeyFiltersFromStrings(raw ...string) (map[string]slog.Level, error) {
	filters := make(map[string]slog.Level, len(raw))

	for _, filter := range raw {
		key, levelStr, found := strings.Cut(filter, "=")
		if !found {
			return nil, fmt.Errorf("invalid filter format: %s, expected key=value", filter)
		}

		var level slog.Level
		if err := level.UnmarshalText([]byte(levelStr)); err != nil {
			return nil, fmt.Errorf("invalid log level in filter %s: %w", filter, err)
		}

		filters[key] = level
	}

	return filters, nil
}
