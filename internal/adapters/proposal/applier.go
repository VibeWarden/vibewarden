package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Applier applies approved proposals to the on-disk configuration file and
// triggers a config reload via the reload service.
//
// It implements ports.ProposalApplier.
type Applier struct {
	configPath string
	reloader   ports.ConfigReloader
}

// NewApplier creates an Applier that modifies the YAML file at configPath and
// triggers a reload via reloader.
func NewApplier(configPath string, reloader ports.ConfigReloader) *Applier {
	return &Applier{
		configPath: configPath,
		reloader:   reloader,
	}
}

// Apply implements ports.ProposalApplier.
// It reads the current YAML file, applies the mutation described by p, writes
// the result back, then triggers a config reload.
func (a *Applier) Apply(ctx context.Context, p proposal.Proposal) error {
	raw, err := os.ReadFile(a.configPath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}
	if doc == nil {
		doc = make(map[string]any)
	}

	switch p.Type {
	case proposal.ActionBlockIP:
		if err := applyBlockIP(doc, p.Params); err != nil {
			return fmt.Errorf("applying block_ip: %w", err)
		}
	case proposal.ActionAdjustRateLimit:
		if err := applyAdjustRateLimit(doc, p.Params); err != nil {
			return fmt.Errorf("applying adjust_rate_limit: %w", err)
		}
	case proposal.ActionUpdateConfig:
		if err := applyUpdateConfig(doc, p.Params); err != nil {
			return fmt.Errorf("applying update_config: %w", err)
		}
	default:
		return fmt.Errorf("unknown action type: %s", p.Type)
	}

	updated, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling updated config: %w", err)
	}

	if err := os.WriteFile(a.configPath, updated, 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	if err := a.reloader.Reload(ctx, "proposal_approved"); err != nil {
		return fmt.Errorf("reloading config: %w", err)
	}

	return nil
}

// applyBlockIP adds ip to the ip_filter.addresses list and ensures the filter
// is enabled in blocklist mode.
func applyBlockIP(doc map[string]any, params map[string]any) error {
	ip, ok := params["ip"].(string)
	if !ok || ip == "" {
		return fmt.Errorf("params.ip must be a non-empty string")
	}

	ipFilter := ensureMap(doc, "ip_filter")

	// Enable ip_filter and ensure blocklist mode.
	ipFilter["enabled"] = true
	if mode, _ := ipFilter["mode"].(string); mode != "blocklist" {
		ipFilter["mode"] = "blocklist"
	}

	// Append ip if not already present.
	addresses := toStringSlice(ipFilter["addresses"])
	for _, a := range addresses {
		if a == ip {
			return nil // already in list
		}
	}
	ipFilter["addresses"] = append(addresses, ip)
	doc["ip_filter"] = ipFilter
	return nil
}

// applyAdjustRateLimit updates rate_limit.per_ip.requests_per_second and burst.
func applyAdjustRateLimit(doc map[string]any, params map[string]any) error {
	rps, rpsOK := toFloat64(params["requests_per_second"])
	burst, burstOK := toInt(params["burst"])

	if !rpsOK && !burstOK {
		return fmt.Errorf("params must include requests_per_second and/or burst")
	}

	rateLimit := ensureMap(doc, "rate_limit")
	perIP := ensureMap(rateLimit, "per_ip")

	if rpsOK {
		if rps <= 0 {
			return fmt.Errorf("requests_per_second must be greater than zero")
		}
		perIP["requests_per_second"] = rps
	}
	if burstOK {
		if burst <= 0 {
			return fmt.Errorf("burst must be greater than zero")
		}
		perIP["burst"] = burst
	}

	rateLimit["per_ip"] = perIP
	doc["rate_limit"] = rateLimit
	return nil
}

// applyUpdateConfig applies a JSON merge-patch expressed in params to the
// document. The params map is treated as the patch; its keys are merged into
// the top-level document.
func applyUpdateConfig(doc map[string]any, params map[string]any) error {
	if len(params) == 0 {
		return fmt.Errorf("params must not be empty for update_config")
	}

	// Round-trip through JSON to normalise types (e.g. json.Number).
	patchJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshaling patch params: %w", err)
	}
	var patch map[string]any
	if err := json.Unmarshal(patchJSON, &patch); err != nil {
		return fmt.Errorf("unmarshaling patch params: %w", err)
	}

	mergePatch(doc, patch)
	return nil
}

// mergePatch applies a JSON merge-patch to target in place.
// For each key in patch: nil value means delete; otherwise recursively merge
// or overwrite.
func mergePatch(target, patch map[string]any) {
	for k, pv := range patch {
		if pv == nil {
			delete(target, k)
			continue
		}
		if pMap, ok := pv.(map[string]any); ok {
			if tMap, ok := target[k].(map[string]any); ok {
				mergePatch(tMap, pMap)
				target[k] = tMap
				continue
			}
		}
		target[k] = pv
	}
}

// ensureMap retrieves the value at key from m as a map[string]any. If the key
// is absent or the value is not a map, a new empty map is created and stored
// back into m.
func ensureMap(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		result := make(map[string]any)
		m[key] = result
		return result
	}
	if result, ok := v.(map[string]any); ok {
		return result
	}
	result := make(map[string]any)
	m[key] = result
	return result
}

// toStringSlice converts v to []string, returning nil when v is nil.
func toStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, elem := range val {
			if s, ok := elem.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// toFloat64 converts v to float64. Returns (0, false) when conversion fails.
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	}
	return 0, false
}

// toInt converts v to int. Returns (0, false) when conversion fails.
func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), val == float64(int(val))
	case json.Number:
		i, err := val.Int64()
		return int(i), err == nil
	}
	return 0, false
}
