package scaffold_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

func TestProjectType_Constants(t *testing.T) {
	tests := []struct {
		name  string
		value scaffold.ProjectType
		want  string
	}{
		{"node", scaffold.ProjectTypeNode, "node"},
		{"go", scaffold.ProjectTypeGo, "go"},
		{"python", scaffold.ProjectTypePython, "python"},
		{"unknown", scaffold.ProjectTypeUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("ProjectType %q = %q, want %q", tt.name, string(tt.value), tt.want)
			}
		})
	}
}

func TestProjectType_Equality(t *testing.T) {
	if scaffold.ProjectTypeNode == scaffold.ProjectTypeGo {
		t.Error("ProjectTypeNode and ProjectTypeGo must be distinct")
	}
	if scaffold.ProjectTypeGo == scaffold.ProjectTypePython {
		t.Error("ProjectTypeGo and ProjectTypePython must be distinct")
	}
	if scaffold.ProjectTypePython == scaffold.ProjectTypeUnknown {
		t.Error("ProjectTypePython and ProjectTypeUnknown must be distinct")
	}
}

func TestProjectConfig_ZeroValue(t *testing.T) {
	var pc scaffold.ProjectConfig

	if pc.Type != "" {
		t.Errorf("zero ProjectConfig.Type = %q, want empty string", pc.Type)
	}
	if pc.DetectedPort != 0 {
		t.Errorf("zero ProjectConfig.DetectedPort = %d, want 0", pc.DetectedPort)
	}
	if pc.HasDockerCompose {
		t.Error("zero ProjectConfig.HasDockerCompose should be false")
	}
	if pc.HasVibeWardenConfig {
		t.Error("zero ProjectConfig.HasVibeWardenConfig should be false")
	}
}

func TestProjectConfig_Construction(t *testing.T) {
	tests := []struct {
		name string
		cfg  scaffold.ProjectConfig
	}{
		{
			name: "node project with detected port",
			cfg: scaffold.ProjectConfig{
				Type:         scaffold.ProjectTypeNode,
				DetectedPort: 3000,
			},
		},
		{
			name: "go project with docker-compose",
			cfg: scaffold.ProjectConfig{
				Type:             scaffold.ProjectTypeGo,
				DetectedPort:     8080,
				HasDockerCompose: true,
			},
		},
		{
			name: "python project with all flags",
			cfg: scaffold.ProjectConfig{
				Type:                scaffold.ProjectTypePython,
				DetectedPort:        5000,
				HasDockerCompose:    true,
				HasVibeWardenConfig: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// value object: a copy must equal the original by field comparison
			copy := tt.cfg
			if copy.Type != tt.cfg.Type {
				t.Errorf("Type mismatch after copy: got %q, want %q", copy.Type, tt.cfg.Type)
			}
			if copy.DetectedPort != tt.cfg.DetectedPort {
				t.Errorf("DetectedPort mismatch after copy: got %d, want %d", copy.DetectedPort, tt.cfg.DetectedPort)
			}
			if copy.HasDockerCompose != tt.cfg.HasDockerCompose {
				t.Errorf("HasDockerCompose mismatch after copy")
			}
			if copy.HasVibeWardenConfig != tt.cfg.HasVibeWardenConfig {
				t.Errorf("HasVibeWardenConfig mismatch after copy")
			}
		})
	}
}

func TestScaffoldOptions_ZeroValue(t *testing.T) {
	var opts scaffold.ScaffoldOptions

	if opts.UpstreamPort != 0 {
		t.Errorf("zero ScaffoldOptions.UpstreamPort = %d, want 0", opts.UpstreamPort)
	}
	if opts.AuthEnabled {
		t.Error("zero ScaffoldOptions.AuthEnabled should be false")
	}
	if opts.RateLimitEnabled {
		t.Error("zero ScaffoldOptions.RateLimitEnabled should be false")
	}
	if opts.TLSEnabled {
		t.Error("zero ScaffoldOptions.TLSEnabled should be false")
	}
	if opts.TLSDomain != "" {
		t.Errorf("zero ScaffoldOptions.TLSDomain = %q, want empty", opts.TLSDomain)
	}
	if opts.Force {
		t.Error("zero ScaffoldOptions.Force should be false")
	}
}

func TestScaffoldOptions_Construction(t *testing.T) {
	tests := []struct {
		name string
		opts scaffold.ScaffoldOptions
	}{
		{
			name: "minimal options",
			opts: scaffold.ScaffoldOptions{UpstreamPort: 8080},
		},
		{
			name: "full options with TLS",
			opts: scaffold.ScaffoldOptions{
				UpstreamPort:     443,
				AuthEnabled:      true,
				RateLimitEnabled: true,
				TLSEnabled:       true,
				TLSDomain:        "example.com",
				Force:            true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := tt.opts
			if copy.UpstreamPort != tt.opts.UpstreamPort {
				t.Errorf("UpstreamPort mismatch: got %d, want %d", copy.UpstreamPort, tt.opts.UpstreamPort)
			}
			if copy.AuthEnabled != tt.opts.AuthEnabled {
				t.Errorf("AuthEnabled mismatch")
			}
			if copy.RateLimitEnabled != tt.opts.RateLimitEnabled {
				t.Errorf("RateLimitEnabled mismatch")
			}
			if copy.TLSEnabled != tt.opts.TLSEnabled {
				t.Errorf("TLSEnabled mismatch")
			}
			if copy.TLSDomain != tt.opts.TLSDomain {
				t.Errorf("TLSDomain mismatch: got %q, want %q", copy.TLSDomain, tt.opts.TLSDomain)
			}
			if copy.Force != tt.opts.Force {
				t.Errorf("Force mismatch")
			}
		})
	}
}

func TestTemplateData_ZeroValue(t *testing.T) {
	var td scaffold.TemplateData

	if td.UpstreamPort != 0 {
		t.Errorf("zero TemplateData.UpstreamPort = %d, want 0", td.UpstreamPort)
	}
	if td.AuthEnabled {
		t.Error("zero TemplateData.AuthEnabled should be false")
	}
	if td.RateLimitEnabled {
		t.Error("zero TemplateData.RateLimitEnabled should be false")
	}
	if td.TLSEnabled {
		t.Error("zero TemplateData.TLSEnabled should be false")
	}
	if td.TLSDomain != "" {
		t.Errorf("zero TemplateData.TLSDomain = %q, want empty", td.TLSDomain)
	}
}

func TestAgentType_Constants(t *testing.T) {
	tests := []struct {
		name  string
		value scaffold.AgentType
		want  string
	}{
		{"claude", scaffold.AgentTypeClaude, "claude"},
		{"cursor", scaffold.AgentTypeCursor, "cursor"},
		{"generic", scaffold.AgentTypeGeneric, "generic"},
		{"all", scaffold.AgentTypeAll, "all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("AgentType %q = %q, want %q", tt.name, string(tt.value), tt.want)
			}
		})
	}
}

func TestAgentType_Distinctness(t *testing.T) {
	agents := []scaffold.AgentType{
		scaffold.AgentTypeClaude,
		scaffold.AgentTypeCursor,
		scaffold.AgentTypeGeneric,
		scaffold.AgentTypeAll,
	}
	seen := make(map[scaffold.AgentType]bool)
	for _, a := range agents {
		if seen[a] {
			t.Errorf("duplicate AgentType value: %q", a)
		}
		seen[a] = true
	}
}

func TestAgentContextData_ZeroValue(t *testing.T) {
	var acd scaffold.AgentContextData

	if acd.UpstreamPort != 0 {
		t.Errorf("zero AgentContextData.UpstreamPort = %d, want 0", acd.UpstreamPort)
	}
	if acd.AuthEnabled {
		t.Error("zero AgentContextData.AuthEnabled should be false")
	}
	if acd.RateLimitEnabled {
		t.Error("zero AgentContextData.RateLimitEnabled should be false")
	}
	if acd.TLSEnabled {
		t.Error("zero AgentContextData.TLSEnabled should be false")
	}
	if acd.RateLimitRPS != 0 {
		t.Errorf("zero AgentContextData.RateLimitRPS = %d, want 0", acd.RateLimitRPS)
	}
	if acd.AdminEnabled {
		t.Error("zero AgentContextData.AdminEnabled should be false")
	}
}

func TestLanguage_Constants(t *testing.T) {
	tests := []struct {
		name  string
		value scaffold.Language
		want  string
	}{
		{"go", scaffold.LanguageGo, "go"},
		{"kotlin", scaffold.LanguageKotlin, "kotlin"},
		{"typescript", scaffold.LanguageTypeScript, "typescript"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("Language %q = %q, want %q", tt.name, string(tt.value), tt.want)
			}
		})
	}
}

func TestLanguage_Distinctness(t *testing.T) {
	if scaffold.LanguageGo == scaffold.LanguageKotlin {
		t.Error("LanguageGo and LanguageKotlin must be distinct")
	}
	if scaffold.LanguageGo == scaffold.LanguageTypeScript {
		t.Error("LanguageGo and LanguageTypeScript must be distinct")
	}
	if scaffold.LanguageKotlin == scaffold.LanguageTypeScript {
		t.Error("LanguageKotlin and LanguageTypeScript must be distinct")
	}
}

func TestInitProjectData_Description(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{"empty description", ""},
		{"non-empty description", "a task management API"},
		{"multi-word description", "real-time analytics dashboard for small businesses"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := scaffold.InitProjectData{
				ProjectName: "myproject",
				ModulePath:  "github.com/org/myproject",
				Port:        3000,
				Language:    scaffold.LanguageGo,
				Description: tt.description,
			}

			if data.Description != tt.description {
				t.Errorf("Description = %q, want %q", data.Description, tt.description)
			}

			// Value object: copy must equal original.
			copy := data
			if copy.Description != data.Description {
				t.Error("Description mismatch after copy")
			}
		})
	}
}

func TestAgentContextData_Construction(t *testing.T) {
	acd := scaffold.AgentContextData{
		UpstreamPort:     8080,
		AuthEnabled:      true,
		RateLimitEnabled: true,
		TLSEnabled:       true,
		RateLimitRPS:     100,
		AdminEnabled:     true,
	}

	if acd.UpstreamPort != 8080 {
		t.Errorf("UpstreamPort = %d, want 8080", acd.UpstreamPort)
	}
	if !acd.AuthEnabled {
		t.Error("AuthEnabled should be true")
	}
	if !acd.RateLimitEnabled {
		t.Error("RateLimitEnabled should be true")
	}
	if !acd.TLSEnabled {
		t.Error("TLSEnabled should be true")
	}
	if acd.RateLimitRPS != 100 {
		t.Errorf("RateLimitRPS = %d, want 100", acd.RateLimitRPS)
	}
	if !acd.AdminEnabled {
		t.Error("AdminEnabled should be true")
	}
}
