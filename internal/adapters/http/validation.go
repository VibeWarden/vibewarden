// Package http implements HTTP handler adapters for VibeWarden's admin API.
package http

import (
	"net/mail"

	"github.com/google/uuid"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ValidateEmail returns ports.ErrInvalidEmail when the supplied email does not
// pass net/mail.ParseAddress validation. It accepts addresses with a display
// name (e.g. "Alice <alice@example.com>") and returns the bare address.
// Returns an empty string and a non-nil error when the address is invalid.
func ValidateEmail(email string) (string, error) {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", ports.ErrInvalidEmail
	}
	return addr.Address, nil
}

// ValidateUUID returns ports.ErrInvalidUUID when id is not a valid UUID.
// Uses github.com/google/uuid.Parse which accepts both upper- and lower-case
// hex digits and the standard hyphenated format.
func ValidateUUID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ports.ErrInvalidUUID
	}
	return nil
}
