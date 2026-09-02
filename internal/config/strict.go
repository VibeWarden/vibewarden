package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnknownKeyError reports keys present in a YAML source that do not map to any
// field in the Config schema. File is the path the keys were read from (empty
// when the keys come from an unnamed source). Keys contains the offending
// dotted paths, sorted alphabetically — for example
// []string{"tls.dmain", "unknown_plugin.enabled"}.
//
// Use errors.As to unwrap this error and inspect File/Keys programmatically.
type UnknownKeyError struct {
	File string
	Keys []string
}

// Error returns a human-readable description of the unknown-key error.
// The message names the offending file (when known) and lists the keys in
// alphabetical order so the output is deterministic across runs.
func (e *UnknownKeyError) Error() string {
	if len(e.Keys) == 0 {
		if e.File != "" {
			return fmt.Sprintf("config %s: unknown key(s)", e.File)
		}
		return "config: unknown key(s)"
	}
	if e.File != "" {
		return fmt.Sprintf("config %s: unknown key(s): %s", e.File, strings.Join(e.Keys, ", "))
	}
	return fmt.Sprintf("config: unknown key(s): %s", strings.Join(e.Keys, ", "))
}

// LoadStrict behaves like Load but additionally rejects any key present in the
// base config file (configPath) or the production-override file
// (prodConfigPath) that does not map to a field in Config. The strict check is
// independent of viper's decoder (ADR-065 keeps ErrorUnused=false on the
// runtime path) — it parses the YAML file(s) directly and walks the
// mapstructure tags on *Config via reflection.
//
// Callers:
//   - vibew validate (always).
//   - vibew bundle, before writing any bundle files.
//
// (vibew deploy was retired in ADR-086 and is no longer a caller.)
//
// The runtime loader (config.Load, used by vibewarden serve) is unchanged for
// forward-compat per ADR-065.
//
// When a file does not exist it is skipped silently, matching config.Load's
// behaviour (viper treats a missing config file as non-fatal). Passing an
// empty configPath or prodConfigPath skips that file.
//
// On unknown keys, LoadStrict returns an error that wraps *UnknownKeyError.
// The error message names the file and the offending dotted keys.
func LoadStrict(configPath, prodConfigPath string) (*Config, error) {
	if configPath != "" {
		if err := checkUnknownKeys(configPath); err != nil {
			return nil, err
		}
	}
	if prodConfigPath != "" {
		if err := checkUnknownKeys(prodConfigPath); err != nil {
			return nil, err
		}
	}

	// Base load uses the same code path as vibew serve so defaults, env-var
	// overrides, and validation rules stay in lock-step with runtime.
	cfg, err := Load(configPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// checkUnknownKeys parses file as YAML and reports any top-level or nested key
// that does not map to a field in *Config. It returns nil when the file does
// not exist (parity with viper's non-fatal handling of missing files) or when
// every key maps cleanly.
func checkUnknownKeys(file string) error {
	data, err := os.ReadFile(file) //nolint:gosec // file is a config path supplied by the caller (vibew validate / deploy)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading config %s: %w", file, err)
	}

	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return fmt.Errorf("parsing config %s: %w", file, err)
	}
	if len(tree) == 0 {
		return nil
	}

	var unknown []string
	walkUnknownKeys(tree, reflect.TypeOf(Config{}), "", &unknown)
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return &UnknownKeyError{File: file, Keys: unknown}
}

// walkUnknownKeys recursively compares the YAML tree against the schema type.
// Any key in tree that has no corresponding mapstructure tag on schema (or any
// nested struct in schema) is appended to out, prefixed by the dotted path
// that leads to it.
//
// Keys whose schema field has mapstructure tag "-" (e.g. DeployMode,
// ProjectRoot) are treated as unknown — they are never expected to appear in
// YAML.
//
// Non-struct schema types accept any child (e.g. map[string]any, interface{},
// map[string]string). In those cases recursion stops — we cannot verify
// arbitrary map values against a fixed schema.
func walkUnknownKeys(tree map[string]any, schema reflect.Type, prefix string, out *[]string) {
	// Unwrap pointers; non-struct schemas accept anything.
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	if schema.Kind() != reflect.Struct {
		return
	}

	fields := mapstructureFields(schema)
	for key, val := range tree {
		field, ok := fields[key]
		if !ok {
			*out = append(*out, joinKey(prefix, key))
			continue
		}

		child, isMap := val.(map[string]any)
		if !isMap {
			continue
		}

		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		// Recurse into nested structs. For slice/map fields we cannot check
		// individual entries without more schema metadata, so we skip them.
		if fieldType.Kind() == reflect.Struct {
			walkUnknownKeys(child, fieldType, joinKey(prefix, key), out)
		}
	}
}

// mapstructureFields returns a map from mapstructure tag → reflect.StructField
// for every exported field of schema. Fields tagged mapstructure:"-" are
// excluded; callers will report them as unknown if they appear in YAML. When a
// field has no mapstructure tag, the lowercased field name is used (matching
// viper's default behaviour).
//
// Embedded structs (anonymous fields) are flattened: their fields appear at the
// parent's level, mirroring mapstructure's default squash behaviour.
func mapstructureFields(schema reflect.Type) map[string]reflect.StructField {
	out := make(map[string]reflect.StructField, schema.NumField())
	for i := 0; i < schema.NumField(); i++ {
		f := schema.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("mapstructure")
		if ok {
			name, opts, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" && strings.Contains(opts, "squash") && f.Anonymous {
				for k, nested := range mapstructureFields(f.Type) {
					out[k] = nested
				}
				continue
			}
			if name != "" {
				out[name] = f
				continue
			}
		}
		if f.Anonymous {
			for k, nested := range mapstructureFields(f.Type) {
				out[k] = nested
			}
			continue
		}
		out[strings.ToLower(f.Name)] = f
	}
	return out
}

// joinKey concatenates a dotted key path. When prefix is empty, key is
// returned unchanged.
func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
