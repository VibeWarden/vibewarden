package secret_test

import (
	"context"
	"errors"
	"os"
	"testing"

	appsecret "github.com/vibewarden/vibewarden/internal/app/secret"
	"github.com/vibewarden/vibewarden/internal/domain/generate"
	domainsecret "github.com/vibewarden/vibewarden/internal/domain/secret"
)

// --- Fakes ---

// fakeSecretStore is a fake ports.SecretStore for testing.
type fakeSecretStore struct {
	healthErr error
	data      map[string]map[string]string // path -> key/values
}

func (f *fakeSecretStore) Get(_ context.Context, path string) (map[string]string, error) {
	if d, ok := f.data[path]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
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
