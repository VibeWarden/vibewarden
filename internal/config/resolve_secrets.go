package config

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	domainsecret "github.com/vibewarden/vibewarden/internal/domain/secret"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ResolveSecrets walks all string fields in cfg using reflection and replaces
// any value starting with "secret://" with the resolved plaintext from store.
//
// The URI format is secret://path/key where the last segment is the key and
// everything before it is the store path. For example,
// secret://auth/google/client_id resolves path "auth/google", key "client_id".
//
// Fields under cfg.Secrets (the bootstrap config section) are skipped because
// the secret store itself is initialised from that section -- it cannot
// reference itself.
//
// ResolveSecrets fails fast on the first resolution error, returning a
// descriptive message that includes the struct field path.
func ResolveSecrets(ctx context.Context, cfg *Config, store ports.SecretKVReader) error {
	return resolveStruct(ctx, reflect.ValueOf(cfg).Elem(), store, "Config")
}

// resolveStruct recursively walks the fields of a struct value. For each
// settable string field whose value starts with "secret://", it resolves the
// URI from the store and replaces the field value with the plaintext.
func resolveStruct(ctx context.Context, v reflect.Value, store ports.SecretKVReader, parentPath string) error {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		fv := v.Field(i)

		// Skip unexported fields.
		if !field.IsExported() {
			continue
		}

		fieldPath := parentPath + "." + field.Name

		// Skip the Secrets section to avoid circular bootstrap.
		if parentPath == "Config" && field.Name == "Secrets" {
			continue
		}

		switch fv.Kind() {
		case reflect.String:
			if err := resolveStringField(ctx, fv, store, fieldPath); err != nil {
				return err
			}

		case reflect.Struct:
			if err := resolveStruct(ctx, fv, store, fieldPath); err != nil {
				return err
			}

		case reflect.Slice:
			if err := resolveSlice(ctx, fv, store, fieldPath); err != nil {
				return err
			}

		case reflect.Map:
			if err := resolveMap(ctx, fv, store, fieldPath); err != nil {
				return err
			}

		case reflect.Ptr:
			if !fv.IsNil() && fv.Elem().Kind() == reflect.Struct {
				if err := resolveStruct(ctx, fv.Elem(), store, fieldPath); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// resolveSlice handles slices: for []string it resolves each element; for
// slices of structs it recurses into each element.
func resolveSlice(ctx context.Context, v reflect.Value, store ports.SecretKVReader, fieldPath string) error {
	for i := range v.Len() {
		elem := v.Index(i)
		elemPath := fmt.Sprintf("%s[%d]", fieldPath, i)

		switch elem.Kind() {
		case reflect.String:
			if err := resolveStringField(ctx, elem, store, elemPath); err != nil {
				return err
			}
		case reflect.Struct:
			if err := resolveStruct(ctx, elem, store, elemPath); err != nil {
				return err
			}
		case reflect.Ptr:
			if !elem.IsNil() && elem.Elem().Kind() == reflect.Struct {
				if err := resolveStruct(ctx, elem.Elem(), store, elemPath); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// resolveMap handles map[string]string fields by resolving secret URIs in
// values. Other map types with string values are handled similarly.
func resolveMap(ctx context.Context, v reflect.Value, store ports.SecretKVReader, fieldPath string) error {
	if v.IsNil() {
		return nil
	}

	for _, key := range v.MapKeys() {
		val := v.MapIndex(key)

		if val.Kind() == reflect.String {
			raw := val.String()
			if domainsecret.IsURI(raw) {
				entryPath := fmt.Sprintf("%s[%s]", fieldPath, key.String())
				resolved, err := resolveURI(ctx, raw, store, entryPath)
				if err != nil {
					return err
				}
				v.SetMapIndex(key, reflect.ValueOf(resolved))
			}
		}
	}
	return nil
}

// resolveStringField checks whether a string field value is a secret:// URI
// and, if so, resolves it from the store and replaces the field value.
func resolveStringField(ctx context.Context, fv reflect.Value, store ports.SecretKVReader, fieldPath string) error {
	raw := fv.String()
	if !domainsecret.IsURI(raw) {
		return nil
	}

	resolved, err := resolveURI(ctx, raw, store, fieldPath)
	if err != nil {
		return err
	}

	fv.SetString(resolved)
	return nil
}

// resolveURI parses a secret:// URI, fetches the data from the store, extracts
// the requested key, and returns the plaintext value.
func resolveURI(ctx context.Context, raw string, store ports.SecretKVReader, fieldPath string) (string, error) {
	uri, err := domainsecret.ParseURI(raw)
	if err != nil {
		return "", fmt.Errorf("config field %s: %w", fieldPath, err)
	}

	data, err := store.Get(ctx, uri.Path())
	if err != nil {
		return "", fmt.Errorf("config field %s: resolving %s: fetching path %q: %w",
			fieldPath, raw, uri.Path(), err)
	}

	value, ok := data[uri.Key()]
	if !ok {
		available := make([]string, 0, len(data))
		for k := range data {
			available = append(available, k)
		}
		sort.Strings(available)
		return "", fmt.Errorf("config field %s: resolving %s: key %q not found at path %q (available keys: %s)",
			fieldPath, raw, uri.Key(), uri.Path(), strings.Join(available, ", "))
	}

	return value, nil
}
