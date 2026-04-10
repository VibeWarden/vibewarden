package middleware

import (
	"context"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestSessionFromContext_NoSession(t *testing.T) {
	ctx := context.Background()

	session, ok := SessionFromContext(ctx)
	if ok {
		t.Error("SessionFromContext on empty context: ok = true, want false")
	}
	if session != nil {
		t.Errorf("SessionFromContext on empty context: session = %v, want nil", session)
	}
}

func TestSessionFromContext_NilSessionStored(t *testing.T) {
	// contextWithSession with a nil pointer should be treated as "no session".
	ctx := contextWithSession(context.Background(), nil)

	session, ok := SessionFromContext(ctx)
	if ok {
		t.Error("SessionFromContext with nil session stored: ok = true, want false")
	}
	if session != nil {
		t.Errorf("SessionFromContext with nil session stored: session = %v, want nil", session)
	}
}

func TestSessionFromContext_ValidSession(t *testing.T) {
	want := &ports.Session{
		ID:     "ses_abc123",
		Active: true,
		Identity: ports.Identity{
			ID:    "usr_xyz",
			Email: "user@example.com",
		},
	}

	ctx := contextWithSession(context.Background(), want)

	session, ok := SessionFromContext(ctx)
	if !ok {
		t.Fatal("SessionFromContext with valid session: ok = false, want true")
	}
	if session == nil {
		t.Fatal("SessionFromContext with valid session: session = nil")
	}
	if session.ID != want.ID {
		t.Errorf("session.ID = %q, want %q", session.ID, want.ID)
	}
	if session.Active != want.Active {
		t.Errorf("session.Active = %v, want %v", session.Active, want.Active)
	}
	if session.Identity.ID != want.Identity.ID {
		t.Errorf("session.Identity.ID = %q, want %q", session.Identity.ID, want.Identity.ID)
	}
	if session.Identity.Email != want.Identity.Email {
		t.Errorf("session.Identity.Email = %q, want %q", session.Identity.Email, want.Identity.Email)
	}
}

func TestContextWithSession_DoesNotMutateParent(t *testing.T) {
	parent := context.Background()
	session := &ports.Session{ID: "ses_parent"}

	child := contextWithSession(parent, session)

	// The parent context must remain unaffected.
	_, ok := SessionFromContext(parent)
	if ok {
		t.Error("parent context should not be affected by contextWithSession")
	}

	// The child context must carry the session.
	got, ok := SessionFromContext(child)
	if !ok {
		t.Error("child context should carry the session")
	}
	if got.ID != session.ID {
		t.Errorf("child session.ID = %q, want %q", got.ID, session.ID)
	}
}

func TestContextWithSession_Overwrite(t *testing.T) {
	first := &ports.Session{ID: "ses_first"}
	second := &ports.Session{ID: "ses_second"}

	ctx := contextWithSession(context.Background(), first)
	ctx = contextWithSession(ctx, second)

	got, ok := SessionFromContext(ctx)
	if !ok {
		t.Fatal("SessionFromContext after overwrite: ok = false, want true")
	}
	if got.ID != second.ID {
		t.Errorf("session.ID = %q, want %q (second session should win)", got.ID, second.ID)
	}
}

func TestContextWithSession_WrongTypeStoredElsewhere(t *testing.T) {
	// Storing an unrelated value under a different key must not affect session retrieval.
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "some value")

	session, ok := SessionFromContext(ctx)
	if ok {
		t.Error("SessionFromContext should return false when no session is stored")
	}
	if session != nil {
		t.Errorf("SessionFromContext should return nil session when nothing stored, got %v", session)
	}
}

// ---------------------------------------------------------------------------
// IdentityFromContext / contextWithIdentity tests
// ---------------------------------------------------------------------------

func TestIdentityFromContext_NoIdentity(t *testing.T) {
	ctx := context.Background()

	ident, ok := IdentityFromContext(ctx)
	if ok {
		t.Error("IdentityFromContext on empty context: ok = true, want false")
	}
	if !ident.IsZero() {
		t.Error("IdentityFromContext on empty context: identity is not zero")
	}
}

func TestIdentityFromContext_ZeroIdentityStored(t *testing.T) {
	// Storing a zero Identity should be treated as "no identity".
	ctx := contextWithIdentity(context.Background(), identity.Identity{})

	ident, ok := IdentityFromContext(ctx)
	if ok {
		t.Error("IdentityFromContext with zero identity stored: ok = true, want false")
	}
	if !ident.IsZero() {
		t.Error("IdentityFromContext with zero identity: returned non-zero identity")
	}
}

func TestIdentityFromContext_ValidIdentity(t *testing.T) {
	want, err := identity.NewIdentity("user-abc", "user@example.com", "kratos", true, nil)
	if err != nil {
		t.Fatalf("NewIdentity() error: %v", err)
	}

	ctx := contextWithIdentity(context.Background(), want)

	got, ok := IdentityFromContext(ctx)
	if !ok {
		t.Fatal("IdentityFromContext with valid identity: ok = false, want true")
	}
	if got.ID() != want.ID() {
		t.Errorf("Identity.ID() = %q, want %q", got.ID(), want.ID())
	}
	if got.Email() != want.Email() {
		t.Errorf("Identity.Email() = %q, want %q", got.Email(), want.Email())
	}
	if got.Provider() != want.Provider() {
		t.Errorf("Identity.Provider() = %q, want %q", got.Provider(), want.Provider())
	}
}

func TestContextWithIdentity_DoesNotMutateParent(t *testing.T) {
	parent := context.Background()
	ident, _ := identity.NewIdentity("user-1", "a@b.com", "kratos", false, nil)

	child := contextWithIdentity(parent, ident)

	// The parent context must remain unaffected.
	_, ok := IdentityFromContext(parent)
	if ok {
		t.Error("parent context should not be affected by contextWithIdentity")
	}

	// The child context must carry the identity.
	got, ok := IdentityFromContext(child)
	if !ok {
		t.Error("child context should carry the identity")
	}
	if got.ID() != ident.ID() {
		t.Errorf("child identity.ID() = %q, want %q", got.ID(), ident.ID())
	}
}
