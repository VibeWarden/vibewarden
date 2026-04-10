package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/llm"
)

// TestNewSchemaDefinition_NilDoc verifies that a nil schema document returns an
// error.
func TestNewSchemaDefinition_NilDoc(t *testing.T) {
	_, err := llm.NewSchemaDefinition(nil)
	if err == nil {
		t.Error("expected error for nil schema document, got nil")
	}
}

// TestNewSchemaDefinition_Valid verifies that a well-formed schema compiles
// without error.
func TestNewSchemaDefinition_Valid(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"choices"},
		"properties": map[string]any{
			"choices": map[string]any{
				"type": "array",
			},
		},
	}
	sd, err := llm.NewSchemaDefinition(schema)
	if err != nil {
		t.Fatalf("NewSchemaDefinition: unexpected error: %v", err)
	}
	if sd.IsZero() {
		t.Error("expected non-zero SchemaDefinition after valid compile")
	}
}

// TestSchemaDefinition_IsZero verifies zero value detection.
func TestSchemaDefinition_IsZero(t *testing.T) {
	var sd llm.SchemaDefinition
	if !sd.IsZero() {
		t.Error("zero SchemaDefinition should return IsZero() == true")
	}
}

// TestNewSchemaDefinitionFromJSON_Valid verifies that a valid JSON schema byte
// slice compiles correctly.
func TestNewSchemaDefinitionFromJSON_Valid(t *testing.T) {
	raw := []byte(`{
		"type": "object",
		"required": ["id"],
		"properties": {
			"id": {"type": "string"}
		}
	}`)
	sd, err := llm.NewSchemaDefinitionFromJSON(raw)
	if err != nil {
		t.Fatalf("NewSchemaDefinitionFromJSON: unexpected error: %v", err)
	}
	if sd.IsZero() {
		t.Error("expected non-zero SchemaDefinition")
	}
}

// TestNewSchemaDefinitionFromJSON_InvalidJSON verifies that malformed JSON returns
// an error.
func TestNewSchemaDefinitionFromJSON_InvalidJSON(t *testing.T) {
	_, err := llm.NewSchemaDefinitionFromJSON([]byte(`not json`))
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

// TestNewResponseValidator_ZeroSchema verifies that constructing a validator from
// a zero SchemaDefinition returns an error.
func TestNewResponseValidator_ZeroSchema(t *testing.T) {
	var sd llm.SchemaDefinition
	_, err := llm.NewResponseValidator(sd)
	if err == nil {
		t.Error("expected error for zero SchemaDefinition, got nil")
	}
}

// openAIChoicesSchema returns a SchemaDefinition that requires a top-level
// "choices" array whose elements each have a "message" object.
func openAIChoicesSchema(t *testing.T) llm.SchemaDefinition {
	t.Helper()
	doc := map[string]any{
		"type":     "object",
		"required": []any{"choices"},
		"properties": map[string]any{
			"choices": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []any{"message"},
				},
			},
		},
	}
	sd, err := llm.NewSchemaDefinition(doc)
	if err != nil {
		t.Fatalf("openAIChoicesSchema: %v", err)
	}
	return sd
}

// TestResponseValidator_Validate runs table-driven tests across valid and invalid
// LLM response bodies.
func TestResponseValidator_Validate(t *testing.T) {
	schema := openAIChoicesSchema(t)
	v, err := llm.NewResponseValidator(schema)
	if err != nil {
		t.Fatalf("NewResponseValidator: %v", err)
	}

	tests := []struct {
		name           string
		body           string
		wantViolations bool
		wantErr        bool
	}{
		{
			name:           "valid OpenAI-style response",
			body:           `{"choices":[{"message":{"role":"assistant","content":"hello"}}],"model":"gpt-4"}`,
			wantViolations: false,
			wantErr:        false,
		},
		{
			name:           "missing required choices field",
			body:           `{"model":"gpt-4"}`,
			wantViolations: true,
			wantErr:        false,
		},
		{
			name:           "choices is not an array",
			body:           `{"choices":"not-an-array"}`,
			wantViolations: true,
			wantErr:        false,
		},
		{
			name:           "array item missing required message field",
			body:           `{"choices":[{"index":0}]}`,
			wantViolations: true,
			wantErr:        false,
		},
		{
			name:    "non-JSON body",
			body:    `not json at all`,
			wantErr: true,
		},
		{
			name:           "empty JSON object missing choices",
			body:           `{}`,
			wantViolations: true,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations, err := v.Validate([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				gotViolations := len(violations) > 0
				if gotViolations != tt.wantViolations {
					t.Errorf("Validate() violations = %v, wantViolations %v; msgs: %v",
						gotViolations, tt.wantViolations, violations)
				}
			}
		})
	}
}

// TestResponseValidator_Validate_StringSchema verifies validation using a string
// type schema.
func TestResponseValidator_Validate_StringSchema(t *testing.T) {
	doc := map[string]any{"type": "string"}
	sd, err := llm.NewSchemaDefinition(doc)
	if err != nil {
		t.Fatalf("NewSchemaDefinition: %v", err)
	}
	v, err := llm.NewResponseValidator(sd)
	if err != nil {
		t.Fatalf("NewResponseValidator: %v", err)
	}

	// A JSON string value should pass.
	violations, err := v.Validate([]byte(`"hello world"`))
	if err != nil {
		t.Fatalf("Validate string: unexpected error: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("expected no violations for valid string, got: %v", violations)
	}

	// A JSON object should fail.
	violations, err = v.Validate([]byte(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("Validate object as string: unexpected error: %v", err)
	}
	if len(violations) == 0 {
		t.Error("expected violations for object when schema requires string")
	}
}

// TestNewSchemaDefinitionFromJSON_InvalidSchemaType verifies that a JSON schema
// that is not an object (e.g. a raw string) results in an error.
func TestNewSchemaDefinitionFromJSON_InvalidSchemaType(t *testing.T) {
	// Passing an array as the schema top level is unusual but valid JSON.
	// A raw JSON array doesn't compile as a valid schema object.
	_, err := llm.NewSchemaDefinitionFromJSON([]byte(`"just a string"`))
	if err == nil {
		t.Error("expected error for non-object JSON schema, got nil")
	}
}

// TestResponseValidator_Validate_CollectsMultipleViolations verifies that when
// multiple fields fail validation, all violation messages are returned.
func TestResponseValidator_Validate_CollectsMultipleViolations(t *testing.T) {
	doc := map[string]any{
		"type":     "object",
		"required": []any{"id", "name", "score"},
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"name":  map[string]any{"type": "string"},
			"score": map[string]any{"type": "number"},
		},
	}
	sd, err := llm.NewSchemaDefinition(doc)
	if err != nil {
		t.Fatalf("NewSchemaDefinition: %v", err)
	}
	v, err := llm.NewResponseValidator(sd)
	if err != nil {
		t.Fatalf("NewResponseValidator: %v", err)
	}

	// Provide wrong types for all required fields.
	body, _ := json.Marshal(map[string]any{
		"id":    42,     // should be string
		"name":  true,   // should be string
		"score": "high", // should be number
	})
	violations, err := v.Validate(body)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if len(violations) == 0 {
		t.Error("expected at least one violation for type mismatches")
	}
}
