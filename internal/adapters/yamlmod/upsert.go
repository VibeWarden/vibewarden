package yamlmod

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// SeedFactory returns the root mapping node for a brand-new YAML file.
// It is invoked by UpsertFields only when the target path does not exist.
// The returned node is the document root (a MappingNode); UpsertFields will
// then apply the caller's edit closure to it before writing.
type SeedFactory func() *yaml.Node

// DiffBuilder is passed to the edit closure of UpsertFields and records
// field-level changes so they can be surfaced to the user. Callers use
// RecordAdd / RecordChange / RecordRemove as they mutate the YAML node tree.
//
// A DiffBuilder is not safe for concurrent use.
type DiffBuilder struct {
	added   []scaffold.FieldChange
	changed []scaffold.FieldChange
	removed []scaffold.FieldChange
}

// NewDiffBuilder returns an empty DiffBuilder.
func NewDiffBuilder() *DiffBuilder { return &DiffBuilder{} }

// RecordAdd records a field addition at the given dotted path with the given
// rendered after-value.
func (b *DiffBuilder) RecordAdd(path, after string) {
	b.added = append(b.added, scaffold.FieldChange{Path: path, After: after})
}

// RecordChange records a scalar value change at path.
func (b *DiffBuilder) RecordChange(path, before, after string) {
	b.changed = append(b.changed, scaffold.FieldChange{Path: path, Before: before, After: after})
}

// RecordRemove records a field removal at path with the old rendered value.
func (b *DiffBuilder) RecordRemove(path, before string) {
	b.removed = append(b.removed, scaffold.FieldChange{Path: path, Before: before})
}

// Build returns the final Diff for the given file path.
func (b *DiffBuilder) Build(file string) scaffold.Diff {
	return scaffold.Diff{
		File:    file,
		Added:   b.added,
		Changed: b.changed,
		Removed: b.removed,
	}
}

// UpsertFields opens path as a yaml.v3 node tree, applies edit on the root
// mapping, and writes the result back atomically. When path does not exist
// and seed is non-nil, seed is invoked to produce an initial root mapping
// before edit runs (so edit always operates on a fully-materialised tree).
//
// If path exists but cannot be parsed as YAML, UpsertFields returns a wrapped
// error ending with "— run `vibew validate` to see details and fix by hand".
// It never regenerates the file from a template in that case — that would
// silently destroy the user's edits.
//
// The edit closure mutates the node tree in place and records field-level
// changes on the supplied DiffBuilder. The returned Diff is built from that
// builder and carries the absolute file path.
//
// Comments, key ordering, and blank lines present in the original file are
// preserved by yaml.v3's node-based marshaller.
func UpsertFields(
	path string,
	seed SeedFactory,
	edit func(root *yaml.Node, diff *DiffBuilder) error,
) (scaffold.Diff, error) {
	builder := NewDiffBuilder()

	data, readErr := os.ReadFile(path) //nolint:gosec // path is a vibewarden.*.yaml resolved from project root
	var doc yaml.Node
	var root *yaml.Node
	switch {
	case readErr == nil:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return scaffold.Diff{}, fmt.Errorf(
				"parsing %s: %w — run `vibew validate` to see details and fix by hand",
				path, err,
			)
		}
		if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
			root = doc.Content[0]
		} else {
			root = mappingNode()
			doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
		}
	case errors.Is(readErr, os.ErrNotExist):
		if seed == nil {
			root = mappingNode()
		} else {
			root = seed()
			if root == nil {
				root = mappingNode()
			}
		}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	default:
		return scaffold.Diff{}, fmt.Errorf("reading %s: %w", path, readErr)
	}

	if err := edit(root, builder); err != nil {
		return scaffold.Diff{}, err
	}

	if err := writeDoc(path, &doc); err != nil {
		return scaffold.Diff{}, err
	}

	return builder.Build(path), nil
}

// writeDoc marshals doc and writes the result to path atomically. It
// preserves document-level head and foot comments (which is where yaml.v3
// attaches trailing commented-out stanzas).
func writeDoc(path string, doc *yaml.Node) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling yaml: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, permConfig); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// UpsertScalar sets a scalar field at root[section][field] to value (tag).
// When section is empty, the field is set at the root mapping directly.
// Missing parent mappings are created. The DiffBuilder records an Add or
// Change depending on whether the field existed before.
//
// The dotted path used for the Diff entry is "section.field" (or "field"
// when section is empty).
func UpsertScalar(root *yaml.Node, builder *DiffBuilder, section, field, value, tag string) {
	target := root
	dotted := field
	if section != "" {
		target = ensureMapping(root, section)
		dotted = section + "." + field
	}

	existing := findKey(target, field)
	if existing == nil {
		appendNode(target, keyNode(field), scalarNode(value, tag))
		if builder != nil {
			builder.RecordAdd(dotted, value)
		}
		return
	}

	before := existing.Value
	if before == value && existing.Kind == yaml.ScalarNode {
		return // no-op: same value, no diff entry
	}
	existing.Kind = yaml.ScalarNode
	existing.Tag = tag
	existing.Value = value
	existing.Content = nil
	if builder != nil {
		builder.RecordChange(dotted, before, value)
	}
}

// ensureMapping returns the value node for key at the top level of root,
// creating an empty mapping (and the key) when it is absent.
func ensureMapping(root *yaml.Node, key string) *yaml.Node {
	if v := findKey(root, key); v != nil {
		if v.Kind != yaml.MappingNode {
			// Coerce the scalar/sequence into an empty mapping — caller asked
			// for a nested field, so the parent must be a mapping.
			v.Kind = yaml.MappingNode
			v.Tag = "!!map"
			v.Value = ""
			v.Content = nil
		}
		return v
	}
	m := mappingNode()
	appendNode(root, keyNode(key), m)
	return m
}

// RenderValue returns a stable string rendering of n for diff display.
// It handles scalars (returns Value), mappings/sequences (returns "<map>" /
// "<seq>"), and nil (returns "").
func RenderValue(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		return "<map>"
	case yaml.SequenceNode:
		return "<seq>"
	default:
		return strings.TrimSpace(n.Value)
	}
}
