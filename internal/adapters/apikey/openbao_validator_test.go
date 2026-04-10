package apikey

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeKeyStore is a test double for KeyStore.
type fakeKeyStore struct {
	data      map[string]string
	err       error
	callCount atomic.Int32
}

func (f *fakeKeyStore) Get(_ context.Context, _ string) (map[string]string, error) {
	f.callCount.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	// Return a copy to prevent mutation.
	out := make(map[string]string, len(f.data))
	for k, v := range f.data {
		out[k] = v
	}
	return out, nil
}

func TestNewOpenBaoValidator_Errors(t *testing.T) {
	tests := []struct {
		name     string
		store    KeyStore
		path     string
		cacheTTL time.Duration
		wantErr  bool
	}{
		{
			name:    "nil store",
			store:   nil,
			path:    "auth/api-keys",
			wantErr: true,
		},
		{
			name:    "empty path",
			store:   &fakeKeyStore{},
			path:    "",
			wantErr: true,
		},
		{
			name:    "valid",
			store:   &fakeKeyStore{},
			path:    "auth/api-keys",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOpenBaoValidator(tt.store, tt.path, tt.cacheTTL, slog.Default())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOpenBaoValidator() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewOpenBaoValidator_DefaultTTL(t *testing.T) {
	v, err := NewOpenBaoValidator(&fakeKeyStore{}, "auth/keys", 0, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.cacheTTL != 5*time.Minute {
		t.Errorf("cacheTTL = %v, want 5m", v.cacheTTL)
	}
}

func TestOpenBaoValidator_Validate_Success(t *testing.T) {
	store := &fakeKeyStore{
		data: map[string]string{
			"ci-deploy":  auth.HashKey("key-ci"),
			"mobile-app": auth.HashKey("key-mobile"),
		},
	}

	v, err := NewOpenBaoValidator(store, "auth/api-keys", time.Minute, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name         string
		plaintextKey string
		wantKeyName  string
	}{
		{"ci key", "key-ci", "ci-deploy"},
		{"mobile key", "key-mobile", "mobile-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := v.Validate(context.Background(), tt.plaintextKey)
			if err != nil {
				t.Fatalf("Validate(%q) unexpected error: %v", tt.plaintextKey, err)
			}
			if got == nil {
				t.Fatal("Validate returned nil key")
			}
			if got.Name != tt.wantKeyName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantKeyName)
			}
			if !got.Active {
				t.Error("key should be active")
			}
		})
	}
}

func TestOpenBaoValidator_Validate_InvalidKey(t *testing.T) {
	store := &fakeKeyStore{
		data: map[string]string{
			"ci-deploy": auth.HashKey("key-ci"),
		},
	}

	v, err := NewOpenBaoValidator(store, "auth/api-keys", time.Minute, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name string
		key  string
	}{
		{"unknown key", "wrong-key"},
		{"empty key", ""},
		{"partial match", "key-c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Validate(context.Background(), tt.key)
			if err == nil {
				t.Errorf("Validate(%q) expected error, got nil", tt.key)
			}
			if !errors.Is(err, ports.ErrAPIKeyInvalid) {
				t.Errorf("Validate(%q) error = %v, want %v", tt.key, err, ports.ErrAPIKeyInvalid)
			}
		})
	}
}

func TestOpenBaoValidator_Validate_EmptyStore(t *testing.T) {
	store := &fakeKeyStore{data: map[string]string{}}

	v, err := NewOpenBaoValidator(store, "auth/api-keys", time.Minute, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = v.Validate(context.Background(), "any-key")
	if err == nil {
		t.Error("expected ErrAPIKeyInvalid when store is empty, got nil")
	}
	if !errors.Is(err, ports.ErrAPIKeyInvalid) {
		t.Errorf("error = %v, want %v", err, ports.ErrAPIKeyInvalid)
	}
}

func TestOpenBaoValidator_Validate_StoreError_NoCache(t *testing.T) {
	store := &fakeKeyStore{err: errors.New("openbao unavailable")}

	v, err := NewOpenBaoValidator(store, "auth/api-keys", time.Minute, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = v.Validate(context.Background(), "some-key")
	if err == nil {
		t.Error("expected error when store fails and cache is empty, got nil")
	}
	// Should not be ErrAPIKeyInvalid — it's a store error.
	if errors.Is(err, ports.ErrAPIKeyInvalid) {
		t.Error("error should not be ErrAPIKeyInvalid for a store failure")
	}
}

func TestOpenBaoValidator_Validate_StoreError_ServeStaleCache(t *testing.T) {
	store := &fakeKeyStore{
		data: map[string]string{
			"ci-deploy": auth.HashKey("key-ci"),
		},
	}

	// Use a very short TTL so the cache expires quickly.
	v, err := NewOpenBaoValidator(store, "auth/api-keys", 10*time.Millisecond, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Warm the cache with a successful call.
	_, err = v.Validate(context.Background(), "key-ci")
	if err != nil {
		t.Fatalf("first Validate failed: %v", err)
	}

	// Now make the store fail.
	store.err = errors.New("openbao unavailable")

	// Wait for TTL to expire.
	time.Sleep(20 * time.Millisecond)

	// Validation should still succeed using stale cache.
	got, err := v.Validate(context.Background(), "key-ci")
	if err != nil {
		t.Fatalf("Validate with stale cache returned error: %v", err)
	}
	if got.Name != "ci-deploy" {
		t.Errorf("Name = %q, want %q", got.Name, "ci-deploy")
	}
}

func TestOpenBaoValidator_CachingReducesStoreCalls(t *testing.T) {
	store := &fakeKeyStore{
		data: map[string]string{
			"ci-deploy": auth.HashKey("key-ci"),
		},
	}

	// Long TTL — cache should not expire during the test.
	v, err := NewOpenBaoValidator(store, "auth/api-keys", time.Minute, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := context.Background()

	// First call loads the cache.
	if _, err := v.Validate(ctx, "key-ci"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}

	// Subsequent calls must reuse the cache.
	for i := 0; i < 5; i++ {
		if _, err := v.Validate(ctx, "key-ci"); err != nil {
			t.Fatalf("Validate call %d: %v", i+2, err)
		}
	}

	// Store should have been called exactly once.
	if got := store.callCount.Load(); got != 1 {
		t.Errorf("store.Get called %d times, want 1", got)
	}
}

func TestOpenBaoValidator_CacheRefreshOnTTLExpiry(t *testing.T) {
	store := &fakeKeyStore{
		data: map[string]string{
			"ci-deploy": auth.HashKey("key-ci"),
		},
	}

	v, err := NewOpenBaoValidator(store, "auth/api-keys", 10*time.Millisecond, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := context.Background()

	// First call — loads cache.
	if _, err := v.Validate(ctx, "key-ci"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}

	// Wait for TTL to expire.
	time.Sleep(20 * time.Millisecond)

	// Second call — must trigger a refresh.
	if _, err := v.Validate(ctx, "key-ci"); err != nil {
		t.Fatalf("second Validate: %v", err)
	}

	if got := store.callCount.Load(); got < 2 {
		t.Errorf("store.Get called %d times after TTL expiry, want >= 2", got)
	}
}

func TestOpenBaoValidator_SkipsInvalidEntries(t *testing.T) {
	// An entry with an empty hash is invalid and should be skipped gracefully.
	store := &fakeKeyStore{
		data: map[string]string{
			"good-key": auth.HashKey("real-key"),
			"":         auth.HashKey("some-key"), // empty name — will be skipped
		},
	}

	v, err := NewOpenBaoValidator(store, "auth/api-keys", time.Minute, slog.Default())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// The good key must still work.
	got, err := v.Validate(context.Background(), "real-key")
	if err != nil {
		t.Fatalf("Validate(good-key) error = %v", err)
	}
	if got.Name != "good-key" {
		t.Errorf("Name = %q, want %q", got.Name, "good-key")
	}
}

// Compile-time assertion that OpenBaoValidator implements ports.APIKeyValidator.
var _ ports.APIKeyValidator = (*OpenBaoValidator)(nil)
