package identity

import (
	"testing"
)

func TestSuccess(t *testing.T) {
	ident, err := NewIdentity("user-123", "alice@example.com", "kratos", true, nil)
	if err != nil {
		t.Fatalf("NewIdentity() unexpected error: %v", err)
	}

	result := Success(ident)

	if !result.Authenticated {
		t.Error("Success().Authenticated = false, want true")
	}
	if result.Reason != "" {
		t.Errorf("Success().Reason = %q, want empty", result.Reason)
	}
	if result.Message != "" {
		t.Errorf("Success().Message = %q, want empty", result.Message)
	}
	if result.Identity.ID() != ident.ID() {
		t.Errorf("Success().Identity.ID() = %q, want %q", result.Identity.ID(), ident.ID())
	}
}

func TestFailure(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		message string
	}{
		{"no credentials", "no_credentials", "no session cookie"},
		{"session invalid", "session_invalid", "session is invalid or expired"},
		{"provider unavailable", "provider_unavailable", "kratos is down"},
		{"token expired", "token_expired", "JWT has expired"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Failure(tt.reason, tt.message)

			if result.Authenticated {
				t.Error("Failure().Authenticated = true, want false")
			}
			if result.Reason != tt.reason {
				t.Errorf("Failure().Reason = %q, want %q", result.Reason, tt.reason)
			}
			if result.Message != tt.message {
				t.Errorf("Failure().Message = %q, want %q", result.Message, tt.message)
			}
			if !result.Identity.IsZero() {
				t.Error("Failure().Identity should be zero value")
			}
		})
	}
}

func TestAuthResult_ZeroValue(t *testing.T) {
	var r AuthResult
	if r.Authenticated {
		t.Error("zero AuthResult.Authenticated = true, want false")
	}
	if !r.Identity.IsZero() {
		t.Error("zero AuthResult.Identity should be zero value")
	}
}
