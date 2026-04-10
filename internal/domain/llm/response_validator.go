package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	jsschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaDefinition holds a JSON Schema used to validate LLM API response bodies.
// It is an immutable value object: once constructed via NewSchemaDefinition it
// can be stored and shared safely across goroutines.
//
// SchemaDefinition accepts a JSON Schema object expressed as a map[string]any.
// The schema must be a valid JSON Schema (any draft supported by
// santhosh-tekuri/jsonschema v6).
type SchemaDefinition struct {
	// compiled is the pre-compiled schema, ready for repeated validation.
	compiled *jsschema.Schema
}

// NewSchemaDefinition compiles a JSON Schema from the provided map and returns
// a SchemaDefinition value object.
//
// schemaDoc must be a non-nil map whose contents form a valid JSON Schema.
// Returns an error when schemaDoc is nil or the schema fails to compile.
func NewSchemaDefinition(schemaDoc map[string]any) (SchemaDefinition, error) {
	if schemaDoc == nil {
		return SchemaDefinition{}, errors.New("schema document cannot be nil")
	}

	c := jsschema.NewCompiler()

	// Add the schema as an in-memory resource so the compiler can reference it.
	const schemaURL = "vibewarden://llm-response-schema"
	if err := c.AddResource(schemaURL, schemaDoc); err != nil {
		return SchemaDefinition{}, fmt.Errorf("adding schema resource: %w", err)
	}

	compiled, err := c.Compile(schemaURL)
	if err != nil {
		return SchemaDefinition{}, fmt.Errorf("compiling schema: %w", err)
	}

	return SchemaDefinition{compiled: compiled}, nil
}

// NewSchemaDefinitionFromJSON compiles a JSON Schema from the provided raw JSON
// bytes and returns a SchemaDefinition value object.
//
// Returns an error when body is not valid JSON, does not unmarshal to an object,
// or the resulting schema fails to compile.
func NewSchemaDefinitionFromJSON(body []byte) (SchemaDefinition, error) {
	var schemaDoc map[string]any
	if err := json.Unmarshal(body, &schemaDoc); err != nil {
		return SchemaDefinition{}, fmt.Errorf("unmarshalling schema JSON: %w", err)
	}
	return NewSchemaDefinition(schemaDoc)
}

// IsZero reports whether the SchemaDefinition is the zero value (no schema configured).
func (s SchemaDefinition) IsZero() bool {
	return s.compiled == nil
}

// ResponseValidator validates JSON response bodies from LLM API upstream responses
// against a pre-compiled JSON schema.
//
// Use NewResponseValidator to construct a properly initialised instance.
// ResponseValidator is safe for concurrent use once constructed.
type ResponseValidator struct {
	schema SchemaDefinition
}

// NewResponseValidator constructs a ResponseValidator that validates against the
// given SchemaDefinition.
//
// Returns an error when schema is the zero value (no schema compiled).
func NewResponseValidator(schema SchemaDefinition) (ResponseValidator, error) {
	if schema.IsZero() {
		return ResponseValidator{}, errors.New("schema definition cannot be zero")
	}
	return ResponseValidator{schema: schema}, nil
}

// Validate validates body against the configured JSON Schema.
//
// It returns (nil, nil) when the body is valid.
// It returns (violations, nil) when the body is invalid JSON-Schema-wise, where
// violations is a non-empty slice of human-readable violation messages.
// It returns (nil, err) only for non-validation errors (e.g. body is not JSON).
//
// The caller should treat any non-empty violations slice as a validation failure
// regardless of the err return value.
func (v ResponseValidator) Validate(body []byte) (violations []string, err error) {
	// Unmarshal the response body into a generic value for schema validation.
	instance, unmarshalErr := jsschema.UnmarshalJSON(strings.NewReader(string(body)))
	if unmarshalErr != nil {
		return nil, fmt.Errorf("unmarshalling response body: %w", unmarshalErr)
	}

	schErr := v.schema.compiled.Validate(instance)
	if schErr == nil {
		return nil, nil
	}

	// Extract leaf violation messages from the ValidationError tree.
	var ve *jsschema.ValidationError
	if errors.As(schErr, &ve) {
		return collectViolations(ve), nil
	}

	// Unexpected error type — surface as a plain violation message.
	return []string{schErr.Error()}, nil
}

// collectViolations walks a ValidationError tree and collects leaf error messages.
// Intermediate nodes that are just wrappers (kind.Schema, kind.Reference) with
// child causes are skipped — only the deepest descriptive messages are returned.
func collectViolations(ve *jsschema.ValidationError) []string {
	if len(ve.Causes) == 0 {
		return []string{ve.Error()}
	}

	var msgs []string
	for _, cause := range ve.Causes {
		msgs = append(msgs, collectViolations(cause)...)
	}
	return msgs
}
