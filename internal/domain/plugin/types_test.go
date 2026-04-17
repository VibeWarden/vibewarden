package plugin

import (
	"testing"

	"github.com/go-viper/mapstructure/v2"
)

// TestPluginConfig_SettingsCaptures_UnknownKeys verifies that the
// mapstructure:",remain" tag on Settings captures plugin-specific keys
// that are not part of the PluginConfig struct fields.
func TestPluginConfig_SettingsCaptures_UnknownKeys(t *testing.T) {
	input := map[string]any{
		"enabled":    true,
		"custom_key": "custom_value",
		"nested": map[string]any{
			"deep": true,
		},
	}

	var cfg PluginConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:  &cfg,
		TagName: "mapstructure",
	})
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	if err := decoder.Decode(input); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Settings == nil {
		t.Fatal("Settings is nil — mapstructure \",remain\" tag not working")
	}
	if cfg.Settings["custom_key"] != "custom_value" {
		t.Errorf("Settings[custom_key] = %v, want %q", cfg.Settings["custom_key"], "custom_value")
	}
	if nested, ok := cfg.Settings["nested"].(map[string]any); !ok || nested["deep"] != true {
		t.Errorf("Settings[nested][deep] = %v, want true", cfg.Settings["nested"])
	}
}
