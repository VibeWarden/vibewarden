package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/promptkickoff"
)

// executePromptTemplate is a helper that runs NewPromptTemplateCmd() with the
// given args against a root command wired with version, and returns (stdout,
// stderr, error).
func executePromptTemplate(t *testing.T, version string, args []string) (stdout, stderr string, err error) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd(version)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"prompt-template"}, args...))

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ---- --help flag ------------------------------------------------------------

func TestPromptTemplateCmd_Help(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v0.0.0-test", []string{"--help"})
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if !strings.Contains(stdout, "prompt-template") {
		t.Errorf("--help output does not mention the command name\n%s", stdout)
	}
}

// ---- exit-1 validation failures --------------------------------------------

func TestPromptTemplateCmd_MissingName_ExitsOne(t *testing.T) {
	_, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--describe", "some app",
	})
	if err == nil {
		t.Fatal("expected error for missing --name, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected error to mention 'name', got: %v", err)
	}
}

func TestPromptTemplateCmd_MissingDescribe_ExitsOne(t *testing.T) {
	_, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--name", "foo",
	})
	if err == nil {
		t.Fatal("expected error for missing --describe, got nil")
	}
	if !strings.Contains(err.Error(), "describe") {
		t.Errorf("expected error to mention 'describe', got: %v", err)
	}
}

func TestPromptTemplateCmd_DeployWithoutDomain_ExitsOne(t *testing.T) {
	_, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--deploy", "--name", "foo", "--describe", "bar",
	})
	if err == nil {
		t.Fatal("expected error for --deploy without --domain, got nil")
	}
	if !errors.Is(err, promptkickoff.ErrDomainRequired) && !strings.Contains(err.Error(), "domain") {
		t.Errorf("expected domain error, got: %v", err)
	}
}

func TestPromptTemplateCmd_DeployWithoutDomain_ErrorMessage(t *testing.T) {
	// The error message must explicitly name both flags so users know exactly
	// what is missing.
	_, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--deploy", "--name", "foo", "--describe", "bar",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// ErrDomainRequired text: "--domain is required when --deploy is set"
	if !strings.Contains(err.Error(), "--domain") || !strings.Contains(err.Error(), "--deploy") {
		t.Errorf("error message should mention both --domain and --deploy, got: %q", err.Error())
	}
}

// ---- stdout cleanliness ----------------------------------------------------

func TestPromptTemplateCmd_FirstLineIsHeader(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--name", "foo", "--describe", "bar",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.SplitN(stdout, "\n", 2)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# VibeWarden Agent Kickoff Prompt") {
		t.Errorf("first line is not the header\ngot: %q", lines[0])
	}
}

func TestPromptTemplateCmd_VersionInHeader(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v1.2.3", []string{
		"--name", "foo", "--describe", "bar",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, "v1.2.3") {
		t.Errorf("output does not contain binary version v1.2.3\n%s", stdout)
	}
}

func TestPromptTemplateCmd_EmptyVersion_FallsBackToDev(t *testing.T) {
	// When the root command has no version (dev build), the command must
	// still render successfully using the "dev" fallback.
	stdout, _, err := executePromptTemplate(t, "", []string{
		"--name", "foo", "--describe", "bar",
	})
	if err != nil {
		t.Fatalf("unexpected error with empty version: %v", err)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("expected 'dev' fallback version in output\n%s", stdout)
	}
}

// ---- key landmarks in rendered output ---------------------------------------

func TestPromptTemplateCmd_DevFlavor_Landmarks(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--name", "myapp", "--describe", "my test app", "--domain", "myapp.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	landmarks := []string{
		"vibew init --name myapp --describe",
		"vibew add tls --domain myapp.example.com",
		"vibew dev",
	}
	for _, lm := range landmarks {
		if !strings.Contains(stdout, lm) {
			t.Errorf("dev output missing landmark %q\n%s", lm, stdout)
		}
	}
}

func TestPromptTemplateCmd_DevFlavor_NoDeploySteps(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--name", "myapp", "--describe", "my test app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(stdout, "vibew bundle") {
		t.Errorf("dev flavor should not contain 'vibew bundle'\n%s", stdout)
	}
	if strings.Contains(stdout, "scp") {
		t.Errorf("dev flavor should not contain 'scp'\n%s", stdout)
	}
}

func TestPromptTemplateCmd_DeployFlavor_Landmarks(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--deploy", "--name", "myapp", "--describe", "my test app", "--domain", "myapp.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	landmarks := []string{
		"vibew init --name myapp --describe",
		"vibew add tls --domain myapp.example.com",
		"vibew dev",
		"vibew bundle",
		"scp -r",
		"docker load -i image.tar",
		"docker compose up -d",
		"/_vibewarden/health",
	}
	for _, lm := range landmarks {
		if !strings.Contains(stdout, lm) {
			t.Errorf("deploy output missing landmark %q\n%s", lm, stdout)
		}
	}
}

func TestPromptTemplateCmd_NoDeployShInAnyFlavor(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"dev", []string{"--name", "foo", "--describe", "bar"}},
		{"deploy", []string{"--deploy", "--name", "foo", "--describe", "bar", "--domain", "foo.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := executePromptTemplate(t, "v0.0.0-test", tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(stdout, "deploy.sh") {
				t.Errorf("%s flavor contains forbidden string 'deploy.sh'\n%s", tt.name, stdout)
			}
		})
	}
}

func TestPromptTemplateCmd_NameSanitised(t *testing.T) {
	stdout, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--name", "My Cool App", "--describe", "bar",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "my-cool-app") {
		t.Errorf("name should be sanitised to 'my-cool-app'\n%s", stdout)
	}
	if strings.Contains(stdout, "My Cool App") {
		t.Errorf("unsanitised name 'My Cool App' should not appear in output\n%s", stdout)
	}
}

func TestPromptTemplateCmd_MultilineDescribe_ExitsOne(t *testing.T) {
	_, _, err := executePromptTemplate(t, "v0.0.0-test", []string{
		"--name", "foo", "--describe", "line1\nline2",
	})
	if err == nil {
		t.Fatal("expected error for multiline --describe, got nil")
	}
}
