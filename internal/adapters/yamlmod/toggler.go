// Package yamlmod provides a ports.FeatureToggler implementation that
// modifies vibewarden.yaml using gopkg.in/yaml.v3 node trees so that YAML
// comments and existing structure are preserved on write.
package yamlmod

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// permConfig is the permission mode for vibewarden.yaml and its temp file.
// Owner-read/write only to protect any credentials stored in the config.
const permConfig = os.FileMode(0o600)

// Toggler implements ports.FeatureToggler by reading and writing
// vibewarden.yaml via yaml.v3 node trees.
type Toggler struct{}

// NewToggler creates a new Toggler.
func NewToggler() *Toggler { return &Toggler{} }

// ReadFeatures parses the vibewarden.yaml at path and returns the current
// feature state.
func (t *Toggler) ReadFeatures(_ context.Context, path string) (*scaffold.FeatureState, error) {
	root, err := readNode(path)
	if err != nil {
		return nil, err
	}

	state := &scaffold.FeatureState{}

	// upstream.port
	state.UpstreamPort = intField(root, "upstream", "port")

	// tls.enabled
	state.TLSEnabled = boolField(root, "tls", "enabled")

	// auth section presence (kratos key present + auth key present)
	state.AuthEnabled = hasKey(root, "auth") || hasKey(root, "kratos")

	// rate_limit.enabled
	state.RateLimitEnabled = boolField(root, "rate_limit", "enabled")

	// admin.enabled
	state.AdminEnabled = boolField(root, "admin", "enabled")

	// metrics.enabled
	state.MetricsEnabled = boolField(root, "metrics", "enabled")

	return state, nil
}

// EnableFeature enables the named feature in the file at path. The file is
// written back atomically (temp file + rename). Returns
// scaffold.ErrFeatureAlreadyEnabled when the feature is already on.
func (t *Toggler) EnableFeature(_ context.Context, path string, feature scaffold.Feature, opts scaffold.FeatureOptions) error {
	root, err := readNode(path)
	if err != nil {
		return err
	}

	switch feature {
	case scaffold.FeatureAuth:
		if hasKey(root, "auth") || hasKey(root, "kratos") {
			return fmt.Errorf("add auth: %w", scaffold.ErrFeatureAlreadyEnabled)
		}
		appendAuthBlock(root)

	case scaffold.FeatureRateLimit:
		if boolField(root, "rate_limit", "enabled") {
			return fmt.Errorf("add rate-limiting: %w", scaffold.ErrFeatureAlreadyEnabled)
		}
		appendRateLimitBlock(root)

	case scaffold.FeatureTLS:
		if boolField(root, "tls", "enabled") {
			return fmt.Errorf("add tls: %w", scaffold.ErrFeatureAlreadyEnabled)
		}
		provider := opts.TLSProvider
		if provider == "" {
			provider = "letsencrypt"
		}
		upsertTLSBlock(root, opts.TLSDomain, provider)

	case scaffold.FeatureAdmin:
		if boolField(root, "admin", "enabled") {
			return fmt.Errorf("add admin: %w", scaffold.ErrFeatureAlreadyEnabled)
		}
		upsertAdminBlock(root)

	case scaffold.FeatureMetrics:
		if boolField(root, "metrics", "enabled") {
			return fmt.Errorf("add metrics: %w", scaffold.ErrFeatureAlreadyEnabled)
		}
		appendMetricsBlock(root)

	default:
		return fmt.Errorf("unknown feature %q", feature)
	}

	return writeNode(path, root)
}

// --------------------------------------------------------------------------
// YAML node helpers
// --------------------------------------------------------------------------

// readNode reads the YAML file at path and returns its document root mapping
// node. Returns scaffold.ErrConfigNotFound when the file does not exist.
func readNode(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is the vibewarden.yaml config file resolved from project root
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", scaffold.ErrConfigNotFound, path)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0], nil
	}
	// Return an empty mapping node when file was empty.
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
}

// writeNode marshals root back to a document node and writes it to path
// atomically (temp file + rename) using 0o644 permissions.
func writeNode(path string, root *yaml.Node) error {
	doc := &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{root},
	}

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

// mappingPairs iterates over a MappingNode's key/value pairs.
// Each call to fn receives the key node and its immediately following value
// node. fn should return false to stop iteration.
func mappingPairs(node *yaml.Node, fn func(key, val *yaml.Node) bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if !fn(node.Content[i], node.Content[i+1]) {
			return
		}
	}
}

// findKey returns the value node for key in the top-level mapping, or nil.
func findKey(root *yaml.Node, key string) *yaml.Node {
	var found *yaml.Node
	mappingPairs(root, func(k, v *yaml.Node) bool {
		if k.Value == key {
			found = v
			return false
		}
		return true
	})
	return found
}

// hasKey returns true when key exists at the top level of root.
func hasKey(root *yaml.Node, key string) bool {
	return findKey(root, key) != nil
}

// boolField returns the bool value at root[section][field], or false.
func boolField(root *yaml.Node, section, field string) bool {
	sec := findKey(root, section)
	if sec == nil {
		return false
	}
	val := findKey(sec, field)
	if val == nil {
		return false
	}
	var b bool
	_ = val.Decode(&b)
	return b
}

// intField returns the int value at root[section][field], or 0.
func intField(root *yaml.Node, section, field string) int {
	sec := findKey(root, section)
	if sec == nil {
		return 0
	}
	val := findKey(sec, field)
	if val == nil {
		return 0
	}
	var n int
	_ = val.Decode(&n)
	return n
}

// setScalar sets or inserts key in root to a scalar value.
func setScalar(root *yaml.Node, key, value, tag string) {
	mappingPairs(root, func(k, v *yaml.Node) bool {
		if k.Value == key {
			v.Kind = yaml.ScalarNode
			v.Tag = tag
			v.Value = value
			return false
		}
		return true
	})
}

// appendNode appends a key/value pair to a mapping node.
func appendNode(m *yaml.Node, key *yaml.Node, value *yaml.Node) {
	m.Content = append(m.Content, key, value)
}

// scalarNode creates a scalar yaml.Node.
func scalarNode(value, tag string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: tag}
}

// keyNode creates a scalar key node (tag !!str).
func keyNode(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: name, Tag: "!!str"}
}

// mappingNode creates an empty mapping node.
func mappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// sequenceNode creates a sequence node with string items.
func sequenceNode(items ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, item := range items {
		n.Content = append(n.Content, scalarNode(item, "!!str"))
	}
	return n
}

// headComment adds a YAML head comment to a node.
func headComment(n *yaml.Node, comment string) *yaml.Node {
	n.HeadComment = comment
	return n
}

// --------------------------------------------------------------------------
// Feature block builders
// --------------------------------------------------------------------------

// appendAuthBlock appends kratos and auth sections to the root mapping.
func appendAuthBlock(root *yaml.Node) {
	// kratos section
	kratosVal := mappingNode()
	appendNode(kratosVal, keyNode("public_url"), scalarNode("http://localhost:4433", "!!str"))
	appendNode(kratosVal, keyNode("admin_url"), scalarNode("http://localhost:4434", "!!str"))
	appendNode(root, headComment(keyNode("kratos"), "# Ory Kratos identity server"), kratosVal)

	// auth section
	authVal := mappingNode()
	appendNode(authVal, keyNode("session_cookie_name"), scalarNode("ory_kratos_session", "!!str"))
	appendNode(authVal, keyNode("login_url"), scalarNode("http://localhost:4433/self-service/login/browser", "!!str"))
	publicPaths := sequenceNode("/health", "/ready")
	appendNode(authVal, keyNode("public_paths"), publicPaths)
	appendNode(root, headComment(keyNode("auth"), "# Authentication (Ory Kratos)"), authVal)
}

// appendRateLimitBlock appends a rate_limit section to root.
func appendRateLimitBlock(root *yaml.Node) {
	rl := mappingNode()
	appendNode(rl, keyNode("enabled"), scalarNode("true", "!!bool"))
	appendNode(rl, keyNode("trust_proxy_headers"), scalarNode("false", "!!bool"))

	perIP := mappingNode()
	appendNode(perIP, keyNode("requests_per_second"), scalarNode("10", "!!int"))
	appendNode(perIP, keyNode("burst"), scalarNode("20", "!!int"))
	appendNode(rl, keyNode("per_ip"), perIP)

	perUser := mappingNode()
	appendNode(perUser, keyNode("requests_per_second"), scalarNode("50", "!!int"))
	appendNode(perUser, keyNode("burst"), scalarNode("100", "!!int"))
	appendNode(rl, keyNode("per_user"), perUser)

	exemptPaths := sequenceNode("/health", "/ready")
	appendNode(rl, keyNode("exempt_paths"), exemptPaths)

	appendNode(root, headComment(keyNode("rate_limit"), "# Rate limiting"), rl)
}

// upsertTLSBlock sets tls.enabled = true plus domain/provider in root.
// If the tls key already exists, its fields are updated; otherwise the section
// is appended.
func upsertTLSBlock(root *yaml.Node, domain, provider string) {
	existing := findKey(root, "tls")
	if existing != nil {
		// Update existing tls section.
		setScalar(existing, "enabled", "true", "!!bool")
		if domain != "" {
			upsertField(existing, "domain", domain, "!!str")
		}
		if provider != "" {
			upsertField(existing, "provider", provider, "!!str")
		}
		// storage_path defaults to /root/.local/share/caddy in config.Load
		// (matches the Docker volume mount). No need to set explicitly.
		return
	}

	tls := mappingNode()
	appendNode(tls, keyNode("enabled"), scalarNode("true", "!!bool"))
	appendNode(tls, keyNode("provider"), scalarNode(provider, "!!str"))
	appendNode(tls, keyNode("domain"), scalarNode(domain, "!!str"))
	appendNode(tls, keyNode("storage_path"), scalarNode("./data/caddy", "!!str"))
	appendNode(root, headComment(keyNode("tls"), "# TLS configuration"), tls)
}

// upsertAdminBlock sets admin.enabled = true and appends a token placeholder.
func upsertAdminBlock(root *yaml.Node) {
	existing := findKey(root, "admin")
	if existing != nil {
		setScalar(existing, "enabled", "true", "!!bool")
		return
	}

	admin := mappingNode()
	appendNode(admin, keyNode("enabled"), scalarNode("true", "!!bool"))
	appendNode(admin, keyNode("token"), scalarNode("${VIBEWARDEN_ADMIN_TOKEN}", "!!str"))
	appendNode(root, headComment(keyNode("admin"), "# Admin API"), admin)
}

// appendMetricsBlock appends a metrics section to root.
func appendMetricsBlock(root *yaml.Node) {
	metrics := mappingNode()
	appendNode(metrics, keyNode("enabled"), scalarNode("true", "!!bool"))
	appendNode(metrics, keyNode("path"), scalarNode("/metrics", "!!str"))
	appendNode(root, headComment(keyNode("metrics"), "# Prometheus metrics"), metrics)
}

// upsertField sets key to value in mapping m; if key does not exist it is
// appended.
func upsertField(m *yaml.Node, key, value, tag string) {
	found := false
	mappingPairs(m, func(k, v *yaml.Node) bool {
		if k.Value == key {
			v.Kind = yaml.ScalarNode
			v.Tag = tag
			v.Value = value
			found = true
			return false
		}
		return true
	})
	if !found {
		appendNode(m, keyNode(key), scalarNode(value, tag))
	}
}
