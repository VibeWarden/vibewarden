package scaffold_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// makeSkillsFS builds a minimal in-memory FS that satisfies fs.ReadFileFS and
// contains stub skill files for all three languages. Used to avoid a dependency
// on the real embedded FS in pure unit tests.
func makeSkillsFS() fs.ReadFileFS {
	return fstest.MapFS{
		"commands/shared/dev.md":       {Data: []byte("run vibew dev")},
		"commands/shared/doctor.md":    {Data: []byte("run vibew doctor")},
		"commands/shared/token.md":     {Data: []byte("run vibew token")},
		"commands/go/test.md":          {Data: []byte("go test ./...")},
		"commands/go/lint.md":          {Data: []byte("golangci-lint run ./...")},
		"commands/go/build.md":         {Data: []byte("go build ./...")},
		"commands/kotlin/test.md":      {Data: []byte("./gradlew test")},
		"commands/kotlin/lint.md":      {Data: []byte("./gradlew ktlintCheck")},
		"commands/kotlin/build.md":     {Data: []byte("./gradlew build")},
		"commands/typescript/test.md":  {Data: []byte("npm test")},
		"commands/typescript/lint.md":  {Data: []byte("npm run lint")},
		"commands/typescript/build.md": {Data: []byte("npm run build")},
	}
}

// TestInitProject_CreatesClaudeCommands_Go verifies that .claude/commands/ is
// created with the expected skill files for a Go project.
func TestInitProject_CreatesClaudeCommands_Go(t *testing.T) {
	svc := scaffoldapp.NewInitProjectService(newFakeRenderer(), makeSkillsFS())

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "goapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	// Shared skills must exist for all languages.
	mustExist(t, parent, "goapp", ".claude", "commands", "dev.md")
	mustExist(t, parent, "goapp", ".claude", "commands", "doctor.md")
	mustExist(t, parent, "goapp", ".claude", "commands", "token.md")

	// Go-specific skills must exist.
	mustExist(t, parent, "goapp", ".claude", "commands", "test.md")
	mustExist(t, parent, "goapp", ".claude", "commands", "lint.md")
	mustExist(t, parent, "goapp", ".claude", "commands", "build.md")
}

// TestInitProject_CreatesClaudeCommands_Kotlin verifies that .claude/commands/ is
// created with the expected skill files for a Kotlin project.
func TestInitProject_CreatesClaudeCommands_Kotlin(t *testing.T) {
	svc := scaffoldapp.NewInitProjectService(newFakeRenderer(), makeSkillsFS())

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "ktapp",
		Language:    domainscaffold.LanguageKotlin,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "ktapp", ".claude", "commands", "dev.md")
	mustExist(t, parent, "ktapp", ".claude", "commands", "doctor.md")
	mustExist(t, parent, "ktapp", ".claude", "commands", "token.md")
	mustExist(t, parent, "ktapp", ".claude", "commands", "test.md")
	mustExist(t, parent, "ktapp", ".claude", "commands", "lint.md")
	mustExist(t, parent, "ktapp", ".claude", "commands", "build.md")
}

// TestInitProject_CreatesClaudeCommands_TypeScript verifies that .claude/commands/
// is created with the expected skill files for a TypeScript project.
func TestInitProject_CreatesClaudeCommands_TypeScript(t *testing.T) {
	svc := scaffoldapp.NewInitProjectService(newFakeRenderer(), makeSkillsFS())

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "tsapp",
		Language:    domainscaffold.LanguageTypeScript,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "tsapp", ".claude", "commands", "dev.md")
	mustExist(t, parent, "tsapp", ".claude", "commands", "doctor.md")
	mustExist(t, parent, "tsapp", ".claude", "commands", "token.md")
	mustExist(t, parent, "tsapp", ".claude", "commands", "test.md")
	mustExist(t, parent, "tsapp", ".claude", "commands", "lint.md")
	mustExist(t, parent, "tsapp", ".claude", "commands", "build.md")
}

// TestInitProject_SkipClaudeCommands_WhenSkillsFSNil verifies that no
// .claude/commands/ directory is created when skillsFS is nil.
func TestInitProject_SkipClaudeCommands_WhenSkillsFSNil(t *testing.T) {
	svc := scaffoldapp.NewInitProjectService(newFakeRenderer(), nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "noskills",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	commandsDir := filepath.Join(parent, "noskills", ".claude", "commands")
	if _, err := os.Stat(commandsDir); err == nil {
		t.Errorf(".claude/commands/ must not be created when skillsFS is nil, but it exists at %s", commandsDir)
	}
}

// TestInitProject_ClaudeCommands_ContentIsCorrect verifies that skill file
// contents are written verbatim from the FS (no template rendering).
func TestInitProject_ClaudeCommands_ContentIsCorrect(t *testing.T) {
	svc := scaffoldapp.NewInitProjectService(newFakeRenderer(), makeSkillsFS())

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "contentcheck",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	tests := []struct {
		skill string
		want  string
	}{
		{"dev.md", "run vibew dev"},
		{"doctor.md", "run vibew doctor"},
		{"token.md", "run vibew token"},
		{"test.md", "go test ./..."},
		{"lint.md", "golangci-lint run ./..."},
		{"build.md", "go build ./..."},
	}

	for _, tt := range tests {
		t.Run(tt.skill, func(t *testing.T) {
			path := filepath.Join(parent, "contentcheck", ".claude", "commands", tt.skill)
			raw, err := os.ReadFile(path) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("reading %s: %v", tt.skill, err)
			}
			if string(raw) != tt.want {
				t.Errorf("%s content = %q, want %q", tt.skill, string(raw), tt.want)
			}
		})
	}
}

// TestInitProject_ClaudeCommands_LanguageSkillsDiffer verifies that the
// language-specific skills (test, lint, build) contain language-appropriate
// commands and not commands from other languages.
func TestInitProject_ClaudeCommands_LanguageSkillsDiffer(t *testing.T) {
	tests := []struct {
		name    string
		lang    domainscaffold.Language
		project string
		// wantTestContains must appear in test.md for this language.
		wantTestContains string
		// wantTestAbsent must NOT appear in test.md for this language.
		wantTestAbsent string
	}{
		{
			name:             "go uses go test",
			lang:             domainscaffold.LanguageGo,
			project:          "gocheck",
			wantTestContains: "go test ./...",
			wantTestAbsent:   "gradlew",
		},
		{
			name:             "kotlin uses gradlew",
			lang:             domainscaffold.LanguageKotlin,
			project:          "ktcheck",
			wantTestContains: "./gradlew test",
			wantTestAbsent:   "go test",
		},
		{
			name:             "typescript uses npm",
			lang:             domainscaffold.LanguageTypeScript,
			project:          "tscheck",
			wantTestContains: "npm test",
			wantTestAbsent:   "go test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := scaffoldapp.NewInitProjectService(newFakeRenderer(), makeSkillsFS())
			parent := t.TempDir()
			opts := scaffoldapp.InitProjectOptions{
				ProjectName: tt.project,
				Language:    tt.lang,
				Port:        3000,
			}

			if err := svc.InitProject(context.Background(), parent, opts); err != nil {
				t.Fatalf("InitProject() unexpected error: %v", err)
			}

			testPath := filepath.Join(parent, tt.project, ".claude", "commands", "test.md")
			raw, err := os.ReadFile(testPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("reading test.md: %v", err)
			}
			content := string(raw)

			if !strings.Contains(content, tt.wantTestContains) {
				t.Errorf("test.md for %s missing %q; got: %s", tt.lang, tt.wantTestContains, content)
			}
			if strings.Contains(content, tt.wantTestAbsent) {
				t.Errorf("test.md for %s must not contain %q; got: %s", tt.lang, tt.wantTestAbsent, content)
			}
		})
	}
}

// TestInitProject_ClaudeCommands_WithRealEmbeddedFS verifies that the real
// embedded skill files are written correctly for all three languages.
func TestInitProject_ClaudeCommands_WithRealEmbeddedFS(t *testing.T) {
	r := mustBuildRealRenderer(t)

	tests := []struct {
		name    string
		lang    domainscaffold.Language
		project string
		// wantTestContains checks test.md for language-specific commands.
		wantTestContains string
		// wantDevContains checks dev.md for the shared vibew dev command.
		wantDevContains string
	}{
		{
			name:             "go",
			lang:             domainscaffold.LanguageGo,
			project:          "realgo",
			wantTestContains: "go test",
			wantDevContains:  "vibew dev",
		},
		{
			name:             "kotlin",
			lang:             domainscaffold.LanguageKotlin,
			project:          "realkt",
			wantTestContains: "gradlew",
			wantDevContains:  "vibew dev",
		},
		{
			name:             "typescript",
			lang:             domainscaffold.LanguageTypeScript,
			project:          "realts",
			wantTestContains: "npm",
			wantDevContains:  "vibew dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := scaffoldapp.NewInitProjectService(r, templates.FS)
			parent := t.TempDir()
			opts := scaffoldapp.InitProjectOptions{
				ProjectName: tt.project,
				Language:    tt.lang,
				Port:        3000,
			}

			if err := svc.InitProject(context.Background(), parent, opts); err != nil {
				t.Fatalf("InitProject() unexpected error: %v", err)
			}

			commandsDir := filepath.Join(parent, tt.project, ".claude", "commands")

			// Verify all six skill files exist.
			for _, skill := range []string{"dev.md", "doctor.md", "token.md", "test.md", "lint.md", "build.md"} {
				mustExist(t, commandsDir, skill)
			}

			devPath := filepath.Join(commandsDir, "dev.md")
			devRaw, err := os.ReadFile(devPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("reading dev.md: %v", err)
			}
			if !strings.Contains(string(devRaw), tt.wantDevContains) {
				t.Errorf("dev.md missing %q; got: %s", tt.wantDevContains, string(devRaw))
			}

			testPath := filepath.Join(commandsDir, "test.md")
			testRaw, err := os.ReadFile(testPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("reading test.md: %v", err)
			}
			if !strings.Contains(string(testRaw), tt.wantTestContains) {
				t.Errorf("test.md for %s missing %q; got: %s", tt.lang, tt.wantTestContains, string(testRaw))
			}
		})
	}
}
