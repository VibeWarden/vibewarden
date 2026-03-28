package templates_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config/templates"
)

// TestOtelCollectorConfig_NoDoubleNamespace verifies that the OTel Collector
// Prometheus exporter config does not include a "namespace: vibewarden" line.
// The OTel adapter already names metrics with the vibewarden_ prefix, so adding
// namespace would produce double-prefixed names like vibewarden_vibewarden_*.
func TestOtelCollectorConfig_NoDoubleNamespace(t *testing.T) {
	data, err := templates.FS.ReadFile("observability/otel-collector-config.yml.tmpl")
	if err != nil {
		t.Fatalf("reading otel-collector-config.yml.tmpl: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "namespace: vibewarden") {
		t.Error("otel-collector-config.yml.tmpl must not contain 'namespace: vibewarden': " +
			"the OTel adapter already prefixes metric names with vibewarden_, adding the " +
			"Prometheus exporter namespace would produce double-prefixed names like " +
			"vibewarden_vibewarden_requests_total")
	}
}

// TestOtelCollectorConfig_PrometheusExporterPresent verifies that the Prometheus
// exporter stanza is still present in the OTel Collector config after removing
// the namespace field.
func TestOtelCollectorConfig_PrometheusExporterPresent(t *testing.T) {
	data, err := templates.FS.ReadFile("observability/otel-collector-config.yml.tmpl")
	if err != nil {
		t.Fatalf("reading otel-collector-config.yml.tmpl: %v", err)
	}
	content := string(data)

	checks := []struct {
		name    string
		present string
	}{
		{"prometheus exporter section", "prometheus:"},
		{"prometheus endpoint", "endpoint: 0.0.0.0:8889"},
		{"const_labels", "const_labels:"},
		{"source label", "source: otel_collector"},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(content, tc.present) {
				t.Errorf("otel-collector-config.yml.tmpl must contain %q", tc.present)
			}
		})
	}
}

// TestDashboard_LokiQueriesUseServiceLabel verifies that all four Loki log panel
// queries in the Grafana dashboard use the OTel-sourced {service="vibewarden"}
// label selector instead of the Promtail-sourced {container="vibewarden"} selector.
// The OTel Collector's Loki exporter maps the service.name resource attribute to the
// "service" label, not the Docker "container" label.
func TestDashboard_LokiQueriesUseServiceLabel(t *testing.T) {
	data, err := templates.FS.ReadFile("observability/vibewarden-dashboard.json")
	if err != nil {
		t.Fatalf("reading vibewarden-dashboard.json: %v", err)
	}
	content := string(data)

	if strings.Contains(content, `container=\"vibewarden\"`) || strings.Contains(content, `container="vibewarden"`) {
		t.Error(`vibewarden-dashboard.json must not contain container="vibewarden" in Loki queries: ` +
			`OTel Collector's Loki exporter uses service.name mapped to the "service" label, ` +
			`not Docker's "container" label`)
	}
}

// TestDashboard_LokiPanelsCovered verifies that all four expected Loki log panels
// use the correct {service="vibewarden"} label selector.
func TestDashboard_LokiPanelsCovered(t *testing.T) {
	data, err := templates.FS.ReadFile("observability/vibewarden-dashboard.json")
	if err != nil {
		t.Fatalf("reading vibewarden-dashboard.json: %v", err)
	}
	content := string(data)

	// The dashboard has 4 Loki log panels (IDs 20, 21, 22, 23). Each should
	// reference the OTel-sourced label. Count occurrences of the correct selector.
	const wantSelector = `service=\"vibewarden\"`
	count := strings.Count(content, wantSelector)
	const wantCount = 4
	if count != wantCount {
		t.Errorf("vibewarden-dashboard.json contains %d occurrence(s) of %q, want %d: "+
			"all 4 Loki log panels (Log Stream, Auth Events, Rate Limit Events, Security Events) "+
			"must use the OTel service label", count, wantSelector, wantCount)
	}
}
