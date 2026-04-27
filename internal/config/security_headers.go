package config

// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
	// Enabled toggles security headers middleware (default: true)
	Enabled bool `mapstructure:"enabled"`

	// HSTSMaxAge is the Strict-Transport-Security max-age in seconds (default: 31536000 = 1 year)
	HSTSMaxAge int `mapstructure:"hsts_max_age"`
	// HSTSIncludeSubDomains includes the includeSubDomains directive (default: true)
	HSTSIncludeSubDomains bool `mapstructure:"hsts_include_subdomains"`
	// HSTSPreload includes the preload directive (default: false — requires manual submission)
	HSTSPreload bool `mapstructure:"hsts_preload"`

	// ContentTypeNosniff sets X-Content-Type-Options: nosniff (default: true)
	ContentTypeNosniff bool `mapstructure:"content_type_nosniff"`

	// FrameOption sets X-Frame-Options value: "DENY", "SAMEORIGIN", or "" to disable (default: "DENY")
	FrameOption string `mapstructure:"frame_option"`

	// ContentSecurityPolicy sets Content-Security-Policy value.
	// An empty string (the default) disables the header entirely; users opt in
	// by setting an explicit policy in vibewarden.yaml.
	// When both ContentSecurityPolicy and CSP are set, ContentSecurityPolicy
	// takes precedence for backward compatibility.
	ContentSecurityPolicy string `mapstructure:"content_security_policy"`

	// CSP holds a structured, declarative Content-Security-Policy configuration.
	// It is used to generate a Content-Security-Policy header value when
	// ContentSecurityPolicy is not set. If ContentSecurityPolicy is non-empty it
	// always takes precedence.
	CSP CSPConfig `mapstructure:"csp"`

	// ReferrerPolicy sets Referrer-Policy value (default: "strict-origin-when-cross-origin")
	ReferrerPolicy string `mapstructure:"referrer_policy"`

	// PermissionsPolicy sets Permissions-Policy value (default: "")
	PermissionsPolicy string `mapstructure:"permissions_policy"`

	// CrossOriginOpenerPolicy sets Cross-Origin-Opener-Policy value (default: "same-origin")
	CrossOriginOpenerPolicy string `mapstructure:"cross_origin_opener_policy"`

	// CrossOriginResourcePolicy sets Cross-Origin-Resource-Policy value (default: "same-origin")
	CrossOriginResourcePolicy string `mapstructure:"cross_origin_resource_policy"`

	// PermittedCrossDomainPolicies sets X-Permitted-Cross-Domain-Policies value (default: "none")
	PermittedCrossDomainPolicies string `mapstructure:"permitted_cross_domain_policies"`

	// SuppressViaHeader removes the Via header from proxied responses (default: true)
	SuppressViaHeader bool `mapstructure:"suppress_via_header"`
}

// CSPConfig holds a declarative Content-Security-Policy configuration.
// Each field maps to a CSP fetch directive and contains the list of allowed
// sources. Sources are written verbatim, so keyword tokens must include their
// surrounding single-quotes (e.g. "'self'", "'none'", "'unsafe-inline'").
// An empty slice means the directive is omitted from the generated header.
type CSPConfig struct {
	// DefaultSrc sets the default-src directive.
	DefaultSrc []string `mapstructure:"default_src"`

	// ScriptSrc sets the script-src directive.
	ScriptSrc []string `mapstructure:"script_src"`

	// StyleSrc sets the style-src directive.
	StyleSrc []string `mapstructure:"style_src"`

	// ImgSrc sets the img-src directive.
	ImgSrc []string `mapstructure:"img_src"`

	// ConnectSrc sets the connect-src directive.
	ConnectSrc []string `mapstructure:"connect_src"`

	// FontSrc sets the font-src directive.
	FontSrc []string `mapstructure:"font_src"`

	// FrameSrc sets the frame-src directive.
	FrameSrc []string `mapstructure:"frame_src"`

	// MediaSrc sets the media-src directive.
	MediaSrc []string `mapstructure:"media_src"`

	// ObjectSrc sets the object-src directive.
	ObjectSrc []string `mapstructure:"object_src"`

	// ManifestSrc sets the manifest-src directive.
	ManifestSrc []string `mapstructure:"manifest_src"`

	// WorkerSrc sets the worker-src directive.
	WorkerSrc []string `mapstructure:"worker_src"`

	// ChildSrc sets the child-src directive.
	ChildSrc []string `mapstructure:"child_src"`

	// FormAction sets the form-action directive.
	FormAction []string `mapstructure:"form_action"`

	// FrameAncestors sets the frame-ancestors directive.
	FrameAncestors []string `mapstructure:"frame_ancestors"`

	// BaseURI sets the base-uri directive.
	BaseURI []string `mapstructure:"base_uri"`
}

// WAFConfig holds Web Application Firewall settings.
type WAFConfig struct {
	// Enabled toggles the WAF rule engine (default: true).
	Enabled bool `mapstructure:"enabled"`

	// Mode controls WAF response to detections: "block" or "detect" (default: "detect").
	Mode string `mapstructure:"mode"`

	// Rules toggles individual rule categories.
	Rules WAFRulesConfig `mapstructure:"rules"`

	// ExemptPaths is a list of URL path glob patterns that bypass WAF scanning.
	ExemptPaths []string `mapstructure:"exempt_paths"`

	// ContentTypeValidation configures the Content-Type validation middleware.
	ContentTypeValidation ContentTypeValidationConfig `mapstructure:"content_type_validation"`

	// AcknowledgeLogMode, when true, suppresses the vibew validate FAIL that
	// fires when WAF is enabled with mode: log in a production config. Use this
	// escape hatch when log mode is intentional for a production rollout. The
	// check still emits an OK row so the acknowledgement is visible in validate
	// output. Default: false.
	AcknowledgeLogMode bool `mapstructure:"acknowledge_log_mode" yaml:"acknowledge_log_mode"`
}

// WAFRulesConfig toggles individual WAF rule categories.
type WAFRulesConfig struct {
	// SQLInjection toggles SQLi detection rules (default: true).
	SQLInjection bool `mapstructure:"sqli"`

	// XSS toggles cross-site scripting detection rules (default: true).
	XSS bool `mapstructure:"xss"`

	// PathTraversal toggles path traversal detection rules (default: true).
	PathTraversal bool `mapstructure:"path_traversal"`

	// CommandInjection toggles command injection detection rules (default: true).
	CommandInjection bool `mapstructure:"command_injection"`
}

// ContentTypeValidationConfig holds settings for the Content-Type validation middleware.
type ContentTypeValidationConfig struct {
	// Enabled toggles Content-Type validation on body-bearing requests (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Allowed is the list of permitted media types.
	// Requests with a Content-Type not in this list receive 415 Unsupported Media Type.
	// Default: ["application/json", "application/x-www-form-urlencoded", "multipart/form-data"]
	Allowed []string `mapstructure:"allowed"`
}

// InputValidationConfig holds request input size limit settings.
type InputValidationConfig struct {
	// Enabled toggles the input validation middleware (default: false).
	Enabled bool `mapstructure:"enabled"`

	// MaxURLLength is the maximum allowed length of the raw request URI in bytes
	// (path + query string). Default: 2048. Zero disables this check.
	MaxURLLength int `mapstructure:"max_url_length"`

	// MaxQueryStringLength is the maximum allowed query string length in bytes,
	// not including the leading "?". Default: 2048. Zero disables this check.
	MaxQueryStringLength int `mapstructure:"max_query_string_length"`

	// MaxHeaderCount is the maximum number of request headers allowed.
	// Default: 100. Zero disables this check.
	MaxHeaderCount int `mapstructure:"max_header_count"`

	// MaxHeaderSize is the maximum allowed byte length of any single header
	// value. Default: 8192. Zero disables this check.
	MaxHeaderSize int `mapstructure:"max_header_size"`

	// PathOverrides defines per-path limit overrides.
	// The first entry whose Path glob pattern (path.Match syntax) matches the
	// request URL path wins. Non-zero fields in the matching entry override the
	// global limits.
	PathOverrides []InputValidationPathOverrideConfig `mapstructure:"path_overrides"`
}

// InputValidationPathOverrideConfig defines per-path limit overrides for the
// input validation middleware.
type InputValidationPathOverrideConfig struct {
	// Path is a glob pattern (path.Match syntax) matched against the request
	// URL path (e.g. "/api/upload", "/static/*").
	Path string `mapstructure:"path"`

	// MaxURLLength overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxURLLength int `mapstructure:"max_url_length"`

	// MaxQueryStringLength overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxQueryStringLength int `mapstructure:"max_query_string_length"`

	// MaxHeaderCount overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxHeaderCount int `mapstructure:"max_header_count"`

	// MaxHeaderSize overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxHeaderSize int `mapstructure:"max_header_size"`
}

// CORSConfig holds Cross-Origin Resource Sharing settings.
type CORSConfig struct {
	// Enabled toggles the CORS plugin (default: false).
	Enabled bool `mapstructure:"enabled"`

	// AllowedOrigins is the list of origins permitted to make cross-origin
	// requests. Use ["*"] to allow all origins (development only).
	AllowedOrigins []string `mapstructure:"allowed_origins"`

	// AllowedMethods is the list of HTTP methods permitted in cross-origin
	// requests (default: GET, POST, PUT, DELETE, OPTIONS).
	AllowedMethods []string `mapstructure:"allowed_methods"`

	// AllowedHeaders is the list of request headers permitted in cross-origin
	// requests (default: Content-Type, Authorization).
	AllowedHeaders []string `mapstructure:"allowed_headers"`

	// ExposedHeaders is the list of response headers exposed to the browser
	// via Access-Control-Expose-Headers (default: []).
	ExposedHeaders []string `mapstructure:"exposed_headers"`

	// AllowCredentials, when true, sets Access-Control-Allow-Credentials: true.
	// Must not be combined with AllowedOrigins: ["*"] (default: false).
	AllowCredentials bool `mapstructure:"allow_credentials"`

	// MaxAge is the number of seconds the browser may cache the preflight
	// response (Access-Control-Max-Age). Zero omits the header (default: 0).
	MaxAge int `mapstructure:"max_age"`
}

// validateCORS validates CORS configuration and returns a slice of error strings.
func validateCORS(c *Config) []string {
	var errs []string
	if c.CORS.Enabled && c.CORS.AllowCredentials {
		for _, o := range c.CORS.AllowedOrigins {
			if o == "*" {
				errs = append(errs, "cors.allow_credentials: true cannot be combined with cors.allowed_origins: [\"*\"]; browsers reject credentialed requests to wildcard origins")
				break
			}
		}
	}
	return errs
}
