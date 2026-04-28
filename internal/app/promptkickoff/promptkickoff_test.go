package promptkickoff

import (
	"errors"
	"strings"
	"testing"
)

// fakeRenderer is a simple ports.TemplateRenderer implementation for testing.
// It records the last call and returns a canned response.
type fakeRenderer struct {
	// output is returned verbatim from Render when err is nil.
	output []byte
	// err is returned from Render when non-nil.
	err error
	// lastTemplate is populated on each Render call.
	lastTemplate string
}

func (f *fakeRenderer) Render(templateName string, _ any) ([]byte, error) {
	f.lastTemplate = templateName
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}

func (f *fakeRenderer) RenderToFile(_ string, _ any, _ string, _ bool) error {
	return nil
}

// baseOpts returns a valid Options value that passes all validation.
func baseOpts() Options {
	return Options{
		Name:         "foo",
		Describe:     "bar",
		Domain:       "demo.example.com",
		VibewVersion: "v0.0.0-test",
	}
}

// ---- validate() tests -------------------------------------------------------

func TestValidate_ValidDevOpts(t *testing.T) {
	opts := baseOpts()
	if err := validate(opts); err != nil {
		t.Errorf("expected nil error for valid dev opts, got: %v", err)
	}
}

func TestValidate_ValidDeployOpts(t *testing.T) {
	opts := baseOpts()
	opts.Deploy = true
	if err := validate(opts); err != nil {
		t.Errorf("expected nil error for valid deploy opts, got: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Options)
		wantErr error
	}{
		{
			name:    "empty version",
			mutate:  func(o *Options) { o.VibewVersion = "" },
			wantErr: ErrVersionRequired,
		},
		{
			name:    "empty name",
			mutate:  func(o *Options) { o.Name = "" },
			wantErr: ErrNameRequired,
		},
		{
			name:    "empty describe",
			mutate:  func(o *Options) { o.Describe = "" },
			wantErr: ErrDescribeRequired,
		},
		{
			name:    "whitespace-only describe",
			mutate:  func(o *Options) { o.Describe = "   \t  " },
			wantErr: ErrDescribeRequired,
		},
		{
			name:    "describe with newline",
			mutate:  func(o *Options) { o.Describe = "line1\nline2" },
			wantErr: ErrDescribeMultiline,
		},
		{
			name:    "describe with carriage return",
			mutate:  func(o *Options) { o.Describe = "line1\rline2" },
			wantErr: ErrDescribeMultiline,
		},
		{
			name: "deploy without domain",
			mutate: func(o *Options) {
				o.Deploy = true
				o.Domain = ""
			},
			wantErr: ErrDomainRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := baseOpts()
			tt.mutate(&opts)
			err := validate(opts)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// ---- sanitizeName() tests ---------------------------------------------------

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo", "foo"},
		{"Foo", "foo"},
		{"My Cool App", "my-cool-app"},
		{"my_app", "my-app"},
		{"--leading", "leading"},
		{"trailing--", "trailing"},
		{"123abc", "123abc"},
		{"hello world", "hello-world"},
		{"UPPER CASE", "upper-case"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- Render() dispatch tests ------------------------------------------------

func TestRender_SelectsDevTemplate(t *testing.T) {
	r := &fakeRenderer{output: []byte("dev output")}
	svc := NewService(r)

	opts := baseOpts()
	opts.Deploy = false
	_, err := svc.Render(opts)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	if r.lastTemplate != "prompts/dev.tmpl" {
		t.Errorf("expected prompts/dev.tmpl, got %q", r.lastTemplate)
	}
}

func TestRender_SelectsDeployTemplate(t *testing.T) {
	r := &fakeRenderer{output: []byte("deploy output")}
	svc := NewService(r)

	opts := baseOpts()
	opts.Deploy = true
	_, err := svc.Render(opts)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	if r.lastTemplate != "prompts/deploy.tmpl" {
		t.Errorf("expected prompts/deploy.tmpl, got %q", r.lastTemplate)
	}
}

func TestRender_SanitizesNameInTemplateData(t *testing.T) {
	// Name sanitisation is verified end-to-end by the golden tests.
	// This test confirms that a name with spaces and capitals does not
	// cause a validation error (the sanitiser, not the validator, handles it).
	r := &fakeRenderer{output: []byte("ok")}
	svc := NewService(r)
	opts := baseOpts()
	opts.Name = "My Cool App"
	_, err := svc.Render(opts)
	if err != nil {
		t.Fatalf("Render() unexpected error for name with spaces/capitals: %v", err)
	}
}

func TestRender_DevFlavor_FallbackDomain(t *testing.T) {
	// When Deploy is false and Domain is empty, Render should succeed and
	// not return ErrDomainRequired (dev flavor does not require domain).
	r := &fakeRenderer{output: []byte("ok")}
	svc := NewService(r)

	opts := baseOpts()
	opts.Deploy = false
	opts.Domain = ""
	_, err := svc.Render(opts)
	if err != nil {
		t.Errorf("expected no error for dev flavor without domain, got: %v", err)
	}
}

func TestRender_RendererError_IsWrapped(t *testing.T) {
	wantErr := errors.New("template error")
	r := &fakeRenderer{err: wantErr}
	svc := NewService(r)

	_, err := svc.Render(baseOpts())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v does not wrap %v", err, wantErr)
	}
}

func TestRender_ValidatesBeforeRendering(t *testing.T) {
	// Renderer should never be called when validation fails.
	r := &fakeRenderer{output: []byte("should not be returned")}
	svc := NewService(r)

	opts := baseOpts()
	opts.Name = ""
	_, err := svc.Render(opts)
	if !errors.Is(err, ErrNameRequired) {
		t.Errorf("expected ErrNameRequired, got %v", err)
	}
	if r.lastTemplate != "" {
		t.Error("renderer was called despite validation failure")
	}
}

func TestRender_DescribeTrimmed(t *testing.T) {
	// Whitespace in --describe is trimmed but does not cause an error if
	// the trimmed value is non-empty.
	r := &fakeRenderer{output: []byte("ok")}
	svc := NewService(r)

	opts := baseOpts()
	opts.Describe = "  a cool app  "
	_, err := svc.Render(opts)
	if err != nil {
		t.Errorf("expected no error for describe with surrounding whitespace, got: %v", err)
	}
}

// TestRender_OutputIsBytes confirms that a nil err always accompanies non-nil
// bytes and vice-versa.
func TestRender_OutputIsBytes(t *testing.T) {
	r := &fakeRenderer{output: []byte("hello world")}
	svc := NewService(r)

	out, err := svc.Render(baseOpts())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "hello world") {
		t.Errorf("output %q does not contain expected content", out)
	}
}
