package builtin_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/builtin"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// generateMasterKey returns a random 32-byte master key for tests.
func generateMasterKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generating master key: %v", err)
	}
	return key
}

func TestNewStore_InvalidKeyLength(t *testing.T) {
	tests := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{"empty key", 0, true},
		{"16 bytes", 16, true},
		{"31 bytes", 31, true},
		{"32 bytes", 32, false},
		{"33 bytes", 33, true},
		{"64 bytes", 64, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keyLen)
			path := filepath.Join(t.TempDir(), "secrets.enc")
			_, err := builtin.NewStore(path, key)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStore() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewStore_EmptyPath(t *testing.T) {
	key := generateMasterKey(t)
	_, err := builtin.NewStore("", key)
	if err == nil {
		t.Error("NewStore() expected error for empty path, got nil")
	}
}

func TestStore_PutAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	// Put a secret.
	data := map[string]string{"username": "admin", "password": "s3cret"}
	if err := store.Put(ctx, "app/db", data); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Get it back.
	got, err := store.Get(ctx, "app/db")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got["username"] != "admin" {
		t.Errorf("Get() username = %q, want %q", got["username"], "admin")
	}
	if got["password"] != "s3cret" {
		t.Errorf("Get() password = %q, want %q", got["password"], "s3cret")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.Get(context.Background(), "nonexistent/path")
	if !errors.Is(err, ports.ErrSecretNotFound) {
		t.Errorf("Get() error = %v, want wrapping ports.ErrSecretNotFound", err)
	}
}

func TestStore_Put_Overwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	// Write initial value.
	if err := store.Put(ctx, "app/key", map[string]string{"v": "1"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Overwrite.
	if err := store.Put(ctx, "app/key", map[string]string{"v": "2"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := store.Get(ctx, "app/key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got["v"] != "2" {
		t.Errorf("Get() v = %q, want %q", got["v"], "2")
	}
}

func TestStore_Delete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	if err := store.Put(ctx, "app/temp", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := store.Delete(ctx, "app/temp"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = store.Get(ctx, "app/temp")
	if !errors.Is(err, ports.ErrSecretNotFound) {
		t.Errorf("Get() after Delete() error = %v, want wrapping ports.ErrSecretNotFound", err)
	}
}

func TestStore_Delete_Nonexistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Deleting a non-existent key should not error.
	if err := store.Delete(context.Background(), "nonexistent"); err != nil {
		t.Errorf("Delete() error = %v, want nil", err)
	}
}

func TestStore_List(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	// Populate some secrets.
	for _, p := range []string{"app/db", "app/redis", "infra/postgres", "app/nested/deep"} {
		if err := store.Put(ctx, p, map[string]string{"k": "v"}); err != nil {
			t.Fatalf("Put(%q) error = %v", p, err)
		}
	}

	tests := []struct {
		name   string
		prefix string
		want   map[string]bool
	}{
		{
			name:   "app prefix",
			prefix: "app/",
			want:   map[string]bool{"db": true, "redis": true, "nested/": true},
		},
		{
			name:   "infra prefix",
			prefix: "infra/",
			want:   map[string]bool{"postgres": true},
		},
		{
			name:   "empty prefix lists all top-level",
			prefix: "",
			want:   map[string]bool{"app/": true, "infra/": true},
		},
		{
			name:   "nonexistent prefix",
			prefix: "other/",
			want:   map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.List(ctx, tt.prefix)
			if err != nil {
				t.Fatalf("List(%q) error = %v", tt.prefix, err)
			}

			gotSet := make(map[string]bool, len(got))
			for _, k := range got {
				gotSet[k] = true
			}

			if len(gotSet) != len(tt.want) {
				t.Errorf("List(%q) returned %d keys, want %d; got %v", tt.prefix, len(gotSet), len(tt.want), got)
			}
			for k := range tt.want {
				if !gotSet[k] {
					t.Errorf("List(%q) missing key %q; got %v", tt.prefix, k, got)
				}
			}
		})
	}
}

func TestStore_Health(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Health(context.Background()); err != nil {
		t.Errorf("Health() error = %v, want nil", err)
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	key := generateMasterKey(t)

	// Create a store, write a secret, then discard the store.
	store1, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	if err := store1.Put(ctx, "persist/test", map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Create a new store instance pointing at the same file.
	store2, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() second instance error = %v", err)
	}

	got, err := store2.Get(ctx, "persist/test")
	if err != nil {
		t.Fatalf("Get() from second instance error = %v", err)
	}
	if got["hello"] != "world" {
		t.Errorf("Get() hello = %q, want %q", got["hello"], "world")
	}
}

func TestStore_WrongKey_CannotDecrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	key1 := generateMasterKey(t)
	key2 := generateMasterKey(t)

	// Create and write with key1.
	store1, err := builtin.NewStore(path, key1)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	if err := store1.Put(ctx, "app/test", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Try to load with key2 -- should fail.
	_, err = builtin.NewStore(path, key2)
	if err == nil {
		t.Error("NewStore() with wrong key expected error, got nil")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	const goroutines = 20

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*3)

	// Concurrent writers.
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "concurrent/" + string(rune('a'+i))
			if err := store.Put(ctx, path, map[string]string{"i": string(rune('0' + i))}); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent readers.
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.List(ctx, "concurrent/")
		}()
	}

	// Concurrent health checks.
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Health(ctx); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent operation error: %v", err)
	}
}

func TestStore_AtomicWrite_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	if err := store.Put(ctx, "app/perm", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("secrets file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify no .tmp file remains.
	tmpFile := path + ".tmp"
	if _, err := os.Stat(tmpFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file %q should not exist after successful write", tmpFile)
	}
}

func TestStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	key := generateMasterKey(t)

	// Create an empty file.
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v, want nil for empty file", err)
	}

	// The store should be empty.
	keys, err := store.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("List() returned %d keys from empty file, want 0", len(keys))
	}
}

func TestStore_EncryptDecryptRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	// Write multiple secrets with various characters.
	secrets := map[string]map[string]string{
		"app/unicode":   {"key": "value-with-unicode-\u00e9\u00e8\u00ea"},
		"app/special":   {"k": "va lue=with+special&chars"},
		"app/empty-val": {"k": ""},
		"app/multikey":  {"a": "1", "b": "2", "c": "3"},
	}

	for p, data := range secrets {
		if err := store.Put(ctx, p, data); err != nil {
			t.Fatalf("Put(%q) error = %v", p, err)
		}
	}

	// Re-load from disk and verify all secrets.
	store2, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() reload error = %v", err)
	}

	for p, wantData := range secrets {
		got, err := store2.Get(ctx, p)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", p, err)
		}
		for k, want := range wantData {
			if got[k] != want {
				t.Errorf("Get(%q)[%q] = %q, want %q", p, k, got[k], want)
			}
		}
	}
}

func TestStore_Get_ReturnsCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	if err := store.Put(ctx, "app/copy", map[string]string{"k": "original"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Get and mutate the returned map.
	got, err := store.Get(ctx, "app/copy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got["k"] = "mutated"

	// Get again -- should still return the original value.
	got2, err := store.Get(ctx, "app/copy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got2["k"] != "original" {
		t.Errorf("Get() after mutation = %q, want %q (internal state was mutated)", got2["k"], "original")
	}
}

func TestStore_Put_StoresCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	data := map[string]string{"k": "original"}
	if err := store.Put(ctx, "app/copy", data); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Mutate the input map after Put.
	data["k"] = "mutated"

	// Get should return the original value, not the mutated one.
	got, err := store.Get(ctx, "app/copy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got["k"] != "original" {
		t.Errorf("Get() = %q, want %q (Put did not copy input)", got["k"], "original")
	}
}

func TestStore_CreatesParentDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep", "dir")
	path := filepath.Join(dir, "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()
	if err := store.Put(ctx, "test", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Verify the file exists.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Stat() error = %v, want file to exist", err)
	}
}

func TestStore_FullFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	key := generateMasterKey(t)

	store, err := builtin.NewStore(path, key)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	// 1. Set some secrets.
	if err := store.Put(ctx, "app/db", map[string]string{"password": "s3cret"}); err != nil {
		t.Fatalf("Put(app/db) error = %v", err)
	}
	if err := store.Put(ctx, "app/redis", map[string]string{"url": "redis://localhost"}); err != nil {
		t.Fatalf("Put(app/redis) error = %v", err)
	}

	// 2. Get.
	got, err := store.Get(ctx, "app/db")
	if err != nil {
		t.Fatalf("Get(app/db) error = %v", err)
	}
	if got["password"] != "s3cret" {
		t.Errorf("Get(app/db) password = %q, want %q", got["password"], "s3cret")
	}

	// 3. List.
	keys, err := store.List(ctx, "app/")
	if err != nil {
		t.Fatalf("List(app/) error = %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("List(app/) returned %d keys, want 2; got %v", len(keys), keys)
	}

	// 4. Delete.
	if err := store.Delete(ctx, "app/redis"); err != nil {
		t.Fatalf("Delete(app/redis) error = %v", err)
	}

	keys, err = store.List(ctx, "app/")
	if err != nil {
		t.Fatalf("List(app/) after delete error = %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("List(app/) after delete returned %d keys, want 1; got %v", len(keys), keys)
	}

	// 5. Health.
	if err := store.Health(ctx); err != nil {
		t.Errorf("Health() error = %v", err)
	}
}
