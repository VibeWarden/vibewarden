package secret_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	appsecret "github.com/vibewarden/vibewarden/internal/app/secret"
	"github.com/vibewarden/vibewarden/internal/domain/generate"
	domainsecret "github.com/vibewarden/vibewarden/internal/domain/secret"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// --- Fakes ---

// fakeSecretStore is a fake ports.SecretStore for testing.
type fakeSecretStore struct {
	healthErr error
	getErr    error                        // when set, every Get returns this error
	getErrAt  map[string]error             // when set, Get at a specific path returns that error (takes precedence over data)
	data      map[string]map[string]string // path -> key/values
}

func (f *fakeSecretStore) Get(_ context.Context, path string) (map[string]string, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if err, ok := f.getErrAt[path]; ok {
		return nil, err
	}
	if d, ok := f.data[path]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("fake: %q: %w", path, ports.ErrSecretNotFound)
}

func (f *fakeSecretStore) Put(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (f *fakeSecretStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (f *fakeSecretStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range f.data {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k[len(prefix):])
		}
	}
	return keys, nil
}

func (f *fakeSecretStore) Health(_ context.Context) error {
	return f.healthErr
}

// fakeCredentialStore is a fake ports.CredentialStore for testing.
type fakeCredentialStore struct {
	creds   *generate.GeneratedCredentials
	readErr error
}

func (f *fakeCredentialStore) Write(_ context.Context, _ *generate.GeneratedCredentials, _ string) error {
	return nil
}

func (f *fakeCredentialStore) Read(_ context.Context, _ string) (*generate.GeneratedCredentials, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.creds, nil
}

// --- Tests ---

func TestService_Get_OpenBaoFirst(t *testing.T) {
	store := &fakeSecretStore{
		data: map[string]map[string]string{
			"infra/postgres": {"password": "pg-secret"},
		},
	}
	credStore := &fakeCredentialStore{
		creds: &generate.GeneratedCredentials{PostgresPassword: "file-password"},
	}
	svc := appsecret.NewService(store, credStore, "/tmp")

	got, err := svc.Get(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Source != domainsecret.SourceOpenBao {
		t.Errorf("Source = %q, want %q", got.Source, domainsecret.SourceOpenBao)
	}
	if got.Data["password"] != "pg-secret" {
		t.Errorf("Data[password] = %q, want %q", got.Data["password"], "pg-secret")
	}
	if got.Alias != "postgres" {
		t.Errorf("Alias = %q, want %q", got.Alias, "postgres")
	}
}

func TestService_Get_FallbackToCredentials(t *testing.T) {
	store := &fakeSecretStore{
		healthErr: errors.New("connection refused"),
	}
	credStore := &fakeCredentialStore{
		creds: &generate.GeneratedCredentials{PostgresPassword: "file-password"},
	}
	svc := appsecret.NewService(store, credStore, "/tmp")

	got, err := svc.Get(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Source != domainsecret.SourceCredentialsFile {
		t.Errorf("Source = %q, want %q", got.Source, domainsecret.SourceCredentialsFile)
	}
	if got.Data["password"] != "file-password" {
		t.Errorf("Data[password] = %q, want %q", got.Data["password"], "file-password")
	}
}

func TestService_Get_ArbitraryPath(t *testing.T) {
	store := &fakeSecretStore{
		data: map[string]map[string]string{
			"demo/api-key": {"key": "abc123"},
		},
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	got, err := svc.Get(context.Background(), "demo/api-key")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Source != domainsecret.SourceOpenBao {
		t.Errorf("Source = %q, want %q", got.Source, domainsecret.SourceOpenBao)
	}
	if got.Data["key"] != "abc123" {
		t.Errorf("Data[key] = %q, want %q", got.Data["key"], "abc123")
	}
	if got.Alias != "" {
		t.Errorf("Alias = %q, want empty", got.Alias)
	}
}

func TestService_Get_ArbitraryPath_NoOpenBao(t *testing.T) {
	store := &fakeSecretStore{
		healthErr: errors.New("not running"),
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	_, err := svc.Get(context.Background(), "demo/api-key")
	if err == nil {
		t.Fatal("Get() expected error for arbitrary path with no OpenBao, got nil")
	}
	if !errors.Is(err, appsecret.ErrNoSourceAvailable) {
		t.Errorf("error = %v, want ErrNoSourceAvailable", err)
	}
}

func TestService_Get_ErrNoSourceAvailable(t *testing.T) {
	store := &fakeSecretStore{
		healthErr: errors.New("not running"),
	}
	credStore := &fakeCredentialStore{
		readErr: os.ErrNotExist,
	}
	svc := appsecret.NewService(store, credStore, "/tmp")

	_, err := svc.Get(context.Background(), "postgres")
	if err == nil {
		t.Fatal("Get() expected error, got nil")
	}
	if !errors.Is(err, appsecret.ErrNoSourceAvailable) {
		t.Errorf("error = %v, want ErrNoSourceAvailable", err)
	}
}

func TestService_Get_OpenBaoAlias_NoOpenBaoPath(t *testing.T) {
	// The "openbao" alias has no OpenBaoPath — must read from .credentials.
	store := &fakeSecretStore{} // healthy, but shouldn't be called
	credStore := &fakeCredentialStore{
		creds: &generate.GeneratedCredentials{OpenBaoDevRootToken: "my-root-token"},
	}
	svc := appsecret.NewService(store, credStore, "/tmp")

	got, err := svc.Get(context.Background(), "openbao")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Source != domainsecret.SourceCredentialsFile {
		t.Errorf("Source = %q, want %q", got.Source, domainsecret.SourceCredentialsFile)
	}
	if got.Data["dev_root_token"] != "my-root-token" {
		t.Errorf("Data[dev_root_token] = %q, want %q", got.Data["dev_root_token"], "my-root-token")
	}
}

func TestService_Get_DynamicCredentials(t *testing.T) {
	// postgres has a DynamicRole — should try database/creds/app-readwrite first.
	store := &fakeSecretStore{
		data: map[string]map[string]string{
			"database/creds/app-readwrite": {
				"username": "v-app-xyz",
				"password": "dyn-password",
			},
		},
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	got, err := svc.Get(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Source != domainsecret.SourceOpenBao {
		t.Errorf("Source = %q, want %q", got.Source, domainsecret.SourceOpenBao)
	}
	if got.Data["username"] != "v-app-xyz" {
		t.Errorf("Data[username] = %q, want %q", got.Data["username"], "v-app-xyz")
	}
}

func TestService_List_MergesSources(t *testing.T) {
	store := &fakeSecretStore{
		data: map[string]map[string]string{
			"infra/postgres": {"password": "x"},
			"app/api-key":    {"key": "y"},
		},
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	paths, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}

	// Must contain all 4 well-known aliases plus the OpenBao paths.
	wantPresent := []string{"postgres", "kratos", "grafana", "openbao", "infra/postgres", "app/api-key"}
	found := make(map[string]bool, len(paths))
	for _, p := range paths {
		found[p] = true
	}
	for _, want := range wantPresent {
		if !found[want] {
			t.Errorf("List() missing path %q; got: %v", want, paths)
		}
	}
}

func TestService_List_NoOpenBao(t *testing.T) {
	store := &fakeSecretStore{
		healthErr: errors.New("not running"),
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	paths, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	// Must still return the 4 well-known aliases.
	if len(paths) < 4 {
		t.Errorf("List() returned %d paths, want at least 4", len(paths))
	}
}

func TestService_List_Sorted(t *testing.T) {
	store := &fakeSecretStore{}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	paths, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i] < paths[i-1] {
			t.Errorf("List() not sorted at index %d: %q > %q", i, paths[i-1], paths[i])
		}
	}
}

// TestService_Get_OpenBaoTransportErrorPropagates is the regression test for
// the silent-masking bug (#812). Before the fix, tryOpenBao converted every
// SecretStore.Get error into nil,nil — an operator running against a
// misconfigured OpenBao (wrong token, network blip, auth expired) would see
// the Service silently fall back to .credentials and never learn the primary
// store was broken. After the fix, only wrapped ErrSecretNotFound triggers
// fallback; any other error propagates up.
func TestService_Get_OpenBaoTransportErrorPropagates(t *testing.T) {
	transportErr := errors.New("connection refused")
	store := &fakeSecretStore{
		getErr: transportErr, // Health() is still healthy (nil); Get() fails hard.
	}
	credStore := &fakeCredentialStore{
		creds: &generate.GeneratedCredentials{
			PostgresPassword: "fallback-should-not-be-used",
		},
	}
	svc := appsecret.NewService(store, credStore, "/tmp")

	_, err := svc.Get(context.Background(), "postgres")
	if err == nil {
		t.Fatal("Get() returned nil error; want the transport error propagated")
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("Get() error = %v; want errors.Is(err, transportErr) == true", err)
	}
	if errors.Is(err, appsecret.ErrSecretNotFound) {
		t.Error("Get() returned ErrSecretNotFound for a transport failure — masking regression")
	}
}

// TestService_Get_NotFoundFallsBackToCredentials confirms the happy path: a
// wrapped ErrSecretNotFound still triggers the fallback chain.
func TestService_Get_NotFoundFallsBackToCredentials(t *testing.T) {
	store := &fakeSecretStore{
		// no data for "infra/postgres" — fake returns wrapped ErrSecretNotFound.
	}
	credStore := &fakeCredentialStore{
		creds: &generate.GeneratedCredentials{
			PostgresPassword: "cred-file-password",
		},
	}

	// Write a .credentials file so fallback has something to read.
	tmp := t.TempDir()
	credPath := tmp + "/.credentials"
	if err := os.WriteFile(credPath, []byte(`{"postgres_password":"cred-file-password"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc := appsecret.NewService(store, credStore, tmp)

	got, err := svc.Get(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil; want a RetrievedSecret from credentials fallback")
	}
	if got.Source != domainsecret.SourceCredentialsFile {
		t.Errorf("Source = %q, want %q", got.Source, domainsecret.SourceCredentialsFile)
	}
}

// TestService_Get_DynamicTransportErrorPropagates is the regression test for
// the silent-masking bug in tryDynamicCredentials (#832). The fix to #812
// addressed tryOpenBao but left the sibling function's same anti-pattern in
// place. Before the fix, a transport/auth error during dynamic-role retrieval
// silently fell through to the static path and onward to credentials — an
// operator with a revoked database role or a broken dynamic-creds backend
// would see the sidecar quietly use stale credentials and never learn that
// dynamic generation was failing.
func TestService_Get_DynamicTransportErrorPropagates(t *testing.T) {
	transportErr := errors.New("connection refused")
	store := &fakeSecretStore{
		// Dynamic path for postgres (database/creds/app-readwrite) fails hard.
		getErrAt: map[string]error{
			"database/creds/app-readwrite": transportErr,
		},
		// Static path would succeed if we fell through — the test asserts we do NOT.
		data: map[string]map[string]string{
			"infra/postgres": {"password": "static-should-not-be-used"},
		},
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	_, err := svc.Get(context.Background(), "postgres")
	if err == nil {
		t.Fatal("Get() returned nil error; want the dynamic transport error propagated")
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("Get() error = %v; want errors.Is(err, transportErr) == true", err)
	}
	if errors.Is(err, appsecret.ErrSecretNotFound) {
		t.Error("Get() returned ErrSecretNotFound for a dynamic transport failure — masking regression")
	}
}

// TestService_Get_DynamicNotFoundFallsThroughToStatic pins the happy-path
// fallback contract: a wrapped ErrSecretNotFound on the dynamic role path
// should still fall through to the static path (which then succeeds).
func TestService_Get_DynamicNotFoundFallsThroughToStatic(t *testing.T) {
	store := &fakeSecretStore{
		// Dynamic path is empty -> fake returns wrapped ErrSecretNotFound.
		// Static path has data -> should be returned.
		data: map[string]map[string]string{
			"infra/postgres": {"password": "static-password"},
		},
	}
	credStore := &fakeCredentialStore{}
	svc := appsecret.NewService(store, credStore, "/tmp")

	got, err := svc.Get(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil; want the static-path secret")
	}
	if got.Source != domainsecret.SourceOpenBao {
		t.Errorf("Source = %q, want %q (static OpenBao path)", got.Source, domainsecret.SourceOpenBao)
	}
	if got.Data["password"] != "static-password" {
		t.Errorf("Data[password] = %q, want %q", got.Data["password"], "static-password")
	}
}
