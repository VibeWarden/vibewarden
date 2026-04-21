package yamlmod_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/adapters/yamlmod"
)

func TestUpsertFields_PreservesCommentsAndOrdering(t *testing.T) {
	// Fixture: YAML with head, line, foot comments, blank lines, commented
	// stanzas, and nested maps. UpsertFields edits a single leaf and must
	// leave everything else byte-identical.
	original := `# top-of-file head comment
# second head line
server:
  host: "127.0.0.1"
  port: 8080        # inline port comment

# WAF is commented out on purpose — do not delete
# waf:
#   enabled: true
#   mode: detect

tls:
  enabled: true
  provider: letsencrypt
  domain: old.example.com

# auth would go here once Kratos is wired up
# auth:
#   mode: kratos
`

	dir := t.TempDir()
	path := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	diff, err := yamlmod.UpsertFields(path, nil, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		yamlmod.UpsertScalar(root, b, "tls", "domain", "new.example.com", "!!str")
		return nil
	})
	if err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading back: %v", readErr)
	}
	out := string(got)

	// Every comment, commented-out stanza, and blank line from the original
	// must survive.
	mustContain := []string{
		"# top-of-file head comment",
		"# second head line",
		"# inline port comment",
		"# WAF is commented out on purpose — do not delete",
		"# waf:",
		"#   mode: detect",
		"# auth would go here once Kratos is wired up",
		"# auth:",
		"new.example.com",
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("output lost %q\n\n--- got ---\n%s", m, out)
		}
	}
	if strings.Contains(out, "old.example.com") {
		t.Errorf("output still contains old.example.com\n\n%s", out)
	}

	// Diff should record a single Change for tls.domain.
	if len(diff.Changed) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(diff.Changed), diff)
	}
	c := diff.Changed[0]
	if c.Path != "tls.domain" || c.Before != "old.example.com" || c.After != "new.example.com" {
		t.Errorf("unexpected change entry: %+v", c)
	}
	if diff.File != path {
		t.Errorf("Diff.File = %q, want %q", diff.File, path)
	}
}

func TestUpsertFields_CreatesFileFromSeed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brand-new.yaml")

	seed := func() *yaml.Node {
		// Equivalent to: server: { port: 443 }
		server := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		server.Content = append(server.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "port"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "443"},
		)
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "server"},
			server,
		)
		return root
	}

	diff, err := yamlmod.UpsertFields(path, seed, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		yamlmod.UpsertScalar(root, b, "tls", "domain", "example.com", "!!str")
		return nil
	})
	if err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	out := string(got)
	for _, m := range []string{"server:", "port: 443", "tls:", "domain: example.com"} {
		if !strings.Contains(out, m) {
			t.Errorf("output missing %q\n\n%s", m, out)
		}
	}

	// tls.domain is an add (tls section wasn't in seed).
	if len(diff.Added) == 0 {
		t.Errorf("expected at least one Added entry, got %+v", diff)
	}
}

func TestUpsertFields_RefusesOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n  bad: : :\n"), 0o600); err != nil {
		t.Fatalf("writing broken fixture: %v", err)
	}

	_, err := yamlmod.UpsertFields(path, nil, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "run `vibew validate`") {
		t.Errorf("error should point to vibew validate, got: %v", err)
	}
}

func TestUpsertFields_ReadErrorIsPropagated(t *testing.T) {
	// A directory, not a file, causes a non-NotExist read error.
	dir := t.TempDir()
	_, err := yamlmod.UpsertFields(dir, nil, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected non-NotExist error, got %v", err)
	}
}

func TestUpsertFields_EditErrorSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vibewarden.production.yaml")
	original := "server:\n  port: 8080\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	wantErr := errors.New("boom")
	_, err := yamlmod.UpsertFields(path, nil, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file was modified despite edit error:\n%s", got)
	}
}

func TestUpsertScalar_AddVsChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.yaml")
	if err := os.WriteFile(path, []byte("tls:\n  domain: a\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	diff, err := yamlmod.UpsertFields(path, nil, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		yamlmod.UpsertScalar(root, b, "tls", "domain", "b", "!!str")        // change
		yamlmod.UpsertScalar(root, b, "tls", "email", "x@example", "!!str") // add
		return nil
	})
	if err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}
	if len(diff.Changed) != 1 {
		t.Errorf("expected 1 change, got %+v", diff.Changed)
	}
	if len(diff.Added) != 1 {
		t.Errorf("expected 1 add, got %+v", diff.Added)
	}
}

func TestUpsertScalar_NoOpWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.yaml")
	if err := os.WriteFile(path, []byte("tls:\n  domain: a\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	diff, err := yamlmod.UpsertFields(path, nil, func(root *yaml.Node, b *yamlmod.DiffBuilder) error {
		yamlmod.UpsertScalar(root, b, "tls", "domain", "a", "!!str") // same value
		return nil
	})
	if err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}
	if !diff.IsEmpty() {
		t.Errorf("expected empty diff, got %+v", diff)
	}
}
