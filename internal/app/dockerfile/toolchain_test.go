package dockerfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/dockerfile"
)

func TestDetectToolchain(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string // relative path → content
		wantLang  dockerfile.Lang
		wantMajor int
		wantMinor int
		wantFound bool
		wantSrc   string
	}{
		// Go
		{
			name:      "go.mod happy path",
			files:     map[string]string{"go.mod": "module example.com/app\n\ngo 1.26\n"},
			wantLang:  dockerfile.LangGo,
			wantMajor: 1,
			wantMinor: 26,
			wantFound: true,
			wantSrc:   "go.mod",
		},
		{
			name:      "go.mod with patch version",
			files:     map[string]string{"go.mod": "module example.com/app\n\ngo 1.26.2\n"},
			wantLang:  dockerfile.LangGo,
			wantMajor: 1,
			wantMinor: 26,
			wantFound: true,
			wantSrc:   "go.mod",
		},
		{
			name:      "go.mod no go directive",
			files:     map[string]string{"go.mod": "module example.com/app\n"},
			wantLang:  dockerfile.LangNone,
			wantFound: false,
		},

		// Node — .nvmrc
		{
			name:      ".nvmrc v20.11.0",
			files:     map[string]string{".nvmrc": "v20.11.0\n"},
			wantLang:  dockerfile.LangNode,
			wantMajor: 20,
			wantMinor: 11,
			wantFound: true,
			wantSrc:   ".nvmrc",
		},
		{
			name:      ".nvmrc bare major",
			files:     map[string]string{".nvmrc": "20\n"},
			wantLang:  dockerfile.LangNode,
			wantMajor: 20,
			wantMinor: 0,
			wantFound: true,
			wantSrc:   ".nvmrc",
		},
		{
			name:      ".nvmrc lts/iron skipped",
			files:     map[string]string{".nvmrc": "lts/iron\n"},
			wantLang:  dockerfile.LangNone,
			wantFound: false,
		},
		{
			name:      ".nvmrc LTS/Iron (uppercase) skipped",
			files:     map[string]string{".nvmrc": "LTS/Iron\n"},
			wantLang:  dockerfile.LangNone,
			wantFound: false,
		},

		// Node — package.json engines.node
		{
			name:      "package.json engines.node exact",
			files:     map[string]string{"package.json": `{"engines":{"node":"20.11.0"}}`},
			wantLang:  dockerfile.LangNode,
			wantMajor: 20,
			wantMinor: 11,
			wantFound: true,
			wantSrc:   "package.json#engines.node",
		},
		{
			name:      "package.json engines.node >=20",
			files:     map[string]string{"package.json": `{"engines":{"node":">=20"}}`},
			wantLang:  dockerfile.LangNode,
			wantMajor: 20,
			wantMinor: 0,
			wantFound: true,
			wantSrc:   "package.json#engines.node",
		},
		{
			name:      "package.json engines.node range >=18.0.0 <21",
			files:     map[string]string{"package.json": `{"engines":{"node":">=18.0.0 <21"}}`},
			wantLang:  dockerfile.LangNode,
			wantMajor: 18,
			wantMinor: 0,
			wantFound: true,
			wantSrc:   "package.json#engines.node",
		},
		{
			name:      "package.json no engines.node",
			files:     map[string]string{"package.json": `{"name":"myapp"}`},
			wantLang:  dockerfile.LangNone,
			wantFound: false,
		},

		// Python — pyproject.toml
		{
			name: "pyproject.toml requires-python >=3.11",
			files: map[string]string{
				"pyproject.toml": "[project]\nrequires-python = \">=3.11\"\n",
			},
			wantLang:  dockerfile.LangPython,
			wantMajor: 3,
			wantMinor: 11,
			wantFound: true,
			wantSrc:   "pyproject.toml#requires-python",
		},
		{
			name: "pyproject.toml requires-python range >=3.11,<4",
			files: map[string]string{
				"pyproject.toml": "[project]\nrequires-python = \">=3.11,<4\"\n",
			},
			wantLang:  dockerfile.LangPython,
			wantMajor: 3,
			wantMinor: 11,
			wantFound: true,
			wantSrc:   "pyproject.toml#requires-python",
		},
		{
			name: "pyproject.toml single-quoted requires-python",
			files: map[string]string{
				"pyproject.toml": "[project]\nrequires-python = '>=3.12'\n",
			},
			wantLang:  dockerfile.LangPython,
			wantMajor: 3,
			wantMinor: 12,
			wantFound: true,
			wantSrc:   "pyproject.toml#requires-python",
		},

		// Python — .python-version
		{
			name:      ".python-version 3.11.4",
			files:     map[string]string{".python-version": "3.11.4\n"},
			wantLang:  dockerfile.LangPython,
			wantMajor: 3,
			wantMinor: 11,
			wantFound: true,
			wantSrc:   ".python-version",
		},
		{
			name:      ".python-version 3.11",
			files:     map[string]string{".python-version": "3.11\n"},
			wantLang:  dockerfile.LangPython,
			wantMajor: 3,
			wantMinor: 11,
			wantFound: true,
			wantSrc:   ".python-version",
		},

		// No manifest.
		{
			name:      "empty project root",
			files:     map[string]string{},
			wantLang:  dockerfile.LangNone,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatalf("WriteFile %s: %v", name, err)
				}
			}

			tc, found, err := dockerfile.DetectToolchain(dir)
			if err != nil {
				t.Fatalf("DetectToolchain() unexpected error: %v", err)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if !tt.wantFound {
				return
			}
			if tc.Lang != tt.wantLang {
				t.Errorf("Lang = %q, want %q", tc.Lang, tt.wantLang)
			}
			if tc.Major != tt.wantMajor {
				t.Errorf("Major = %d, want %d", tc.Major, tt.wantMajor)
			}
			if tc.Minor != tt.wantMinor {
				t.Errorf("Minor = %d, want %d", tc.Minor, tt.wantMinor)
			}
			if tt.wantSrc != "" && tc.Source != tt.wantSrc {
				t.Errorf("Source = %q, want %q", tc.Source, tt.wantSrc)
			}
		})
	}
}
