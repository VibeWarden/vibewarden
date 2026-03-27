// Package config provides configuration loading and validation for VibeWarden.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseBodySize parses a human-readable size string and returns the number of bytes.
// Supported units (case-insensitive): B, KB, MB, GB, TB.
// An empty string or "0" returns 0 (no limit).
//
// Examples:
//
//	ParseBodySize("1MB")   → 1048576, nil
//	ParseBodySize("512KB") → 524288, nil
//	ParseBodySize("50MB")  → 52428800, nil
//	ParseBodySize("0")     → 0, nil
//	ParseBodySize("")      → 0, nil
func ParseBodySize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	// Find where the numeric part ends and the unit begins.
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}

	numStr := strings.TrimSpace(s[:i])
	unit := strings.TrimSpace(strings.ToUpper(s[i:]))

	if numStr == "" {
		return 0, fmt.Errorf("invalid body size %q: no numeric value", s)
	}

	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid body size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid body size %q: value must be non-negative", s)
	}

	var multiplier float64
	switch unit {
	case "", "B":
		multiplier = 1
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("invalid body size %q: unknown unit %q (supported: B, KB, MB, GB, TB)", s, unit)
	}

	return int64(n * multiplier), nil
}
