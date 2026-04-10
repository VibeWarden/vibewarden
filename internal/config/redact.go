package config

import (
	"encoding/json"
	"strings"
)

// RedactedConfig is a JSON-serializable map representation of the running
// configuration with sensitive fields masked. It is safe to return to
// external callers such as the admin API.
type RedactedConfig map[string]any

// sensitiveFieldPatterns are substrings that identify sensitive field names.
// Field names containing any of these patterns (case-insensitive) are
// replaced with "[REDACTED]" when producing a RedactedConfig.
var sensitiveFieldPatterns = []string{
	"password", "secret", "key", "token", "credential", "dsn", "url",
}

// isSensitive reports whether the given field name should be redacted.
func isSensitive(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range sensitiveFieldPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// Redact returns a RedactedConfig built from cfg with all sensitive fields
// replaced by "[REDACTED]". Field names are matched case-insensitively
// against sensitiveFieldPatterns.
//
// The redaction is performed by marshalling the config to JSON and walking the
// resulting map, so the output shape matches the YAML/JSON field names used in
// vibewarden.yaml.
func Redact(cfg *Config) RedactedConfig {
	raw, err := json.Marshal(cfg)
	if err != nil {
		// cfg is a well-typed struct; marshalling should never fail.
		return RedactedConfig{"error": "unable to serialize config"}
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return RedactedConfig{"error": "unable to deserialize config"}
	}

	return RedactedConfig(redactMap(m))
}

// redactMap recursively walks a map and redacts sensitive leaf values.
func redactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitive(k) {
			// Only redact non-empty string values to avoid masking false defaults.
			if s, ok := v.(string); ok && s != "" {
				out[k] = "[REDACTED]"
				continue
			}
		}
		switch val := v.(type) {
		case map[string]any:
			out[k] = redactMap(val)
		case []any:
			out[k] = redactSlice(k, val)
		default:
			out[k] = v
		}
	}
	return out
}

// redactSlice walks a JSON array. When the parent key is sensitive, all
// non-empty string elements are redacted. Otherwise the slice elements are
// walked recursively.
func redactSlice(parentKey string, s []any) []any {
	out := make([]any, len(s))
	for i, elem := range s {
		switch val := elem.(type) {
		case map[string]any:
			out[i] = redactMap(val)
		case string:
			if isSensitive(parentKey) && val != "" {
				out[i] = "[REDACTED]"
			} else {
				out[i] = val
			}
		default:
			out[i] = val
		}
	}
	return out
}
