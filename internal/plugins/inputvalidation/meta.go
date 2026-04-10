package inputvalidation

// Description returns a short description of the input validation plugin.
func (p *Plugin) Description() string {
	return "Input validation: enforce URL length, query string length, header count, and header value size limits before WAF scanning"
}

// ConfigSchema returns the configuration field descriptions for the input
// validation plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":                                  "Enable the input validation middleware (default: false)",
		"max_url_length":                           "Maximum allowed raw request URI length in bytes (default: 2048; 0 disables)",
		"max_query_string_length":                  "Maximum allowed query string length in bytes (default: 2048; 0 disables)",
		"max_header_count":                         "Maximum number of request headers allowed (default: 100; 0 disables)",
		"max_header_size":                          "Maximum allowed byte length of any single header value (default: 8192; 0 disables)",
		"path_overrides[].path":                    "URL path glob pattern (path.Match syntax) for this override",
		"path_overrides[].max_url_length":          "Override max URL length for matching paths (0 inherits global value)",
		"path_overrides[].max_query_string_length": "Override max query string length for matching paths (0 inherits global value)",
		"path_overrides[].max_header_count":        "Override max header count for matching paths (0 inherits global value)",
		"path_overrides[].max_header_size":         "Override max header value size for matching paths (0 inherits global value)",
	}
}

// Example returns an example YAML configuration for the input validation plugin.
func (p *Plugin) Example() string {
	return `  input_validation:
    enabled: true
    max_url_length: 2048
    max_query_string_length: 2048
    max_header_count: 100
    max_header_size: 8192
    path_overrides:
      - path: /api/upload
        max_query_string_length: 8192
      - path: /api/search
        max_query_string_length: 4096`
}
