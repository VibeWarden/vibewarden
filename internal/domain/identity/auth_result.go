package identity

// AuthResult represents the outcome of an authentication attempt.
// It contains either a valid Identity or information about why auth failed.
type AuthResult struct {
	// Identity is the authenticated user's identity. Zero value if auth failed.
	Identity Identity

	// Authenticated is true if authentication succeeded.
	Authenticated bool

	// Reason is a machine-readable code explaining auth failure (e.g., "token_expired",
	// "invalid_signature", "session_not_found"). Empty when Authenticated is true.
	Reason string

	// Message is a human-readable description of the failure. Empty when Authenticated is true.
	Message string
}

// Success creates an AuthResult for a successful authentication.
func Success(ident Identity) AuthResult {
	return AuthResult{
		Identity:      ident,
		Authenticated: true,
	}
}

// Failure creates an AuthResult for a failed authentication.
func Failure(reason, message string) AuthResult {
	return AuthResult{
		Authenticated: false,
		Reason:        reason,
		Message:       message,
	}
}
