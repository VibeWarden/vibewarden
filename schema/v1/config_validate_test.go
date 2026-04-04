// Package schema_test validates that config.json is a well-formed JSON Schema
// and that representative vibewarden.yaml documents validate correctly against it.
package schema_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	jsschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// configSchemaPath returns the absolute path to schema/v1/config.json.
func configSchemaPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "config.json")
}

// compileConfigSchema compiles config.json and returns the compiled schema.
func compileConfigSchema(t *testing.T) *jsschema.Schema {
	t.Helper()
	c := jsschema.NewCompiler()
	sch, err := c.Compile(configSchemaPath())
	if err != nil {
		t.Fatalf("compile config schema: %v", err)
	}
	return sch
}

// unmarshalJSON is a helper that parses a JSON string into a value suitable for
// schema validation.
func unmarshalJSON(t *testing.T, s string) any {
	t.Helper()
	inst, err := jsschema.UnmarshalJSON(strings.NewReader(s))
	if err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	return inst
}

// TestConfigSchemaIsValid ensures config.json itself is valid JSON and compiles
// without errors.
func TestConfigSchemaIsValid(t *testing.T) {
	compileConfigSchema(t)
}

// TestConfigSchemaAcceptsValidDocuments verifies that representative
// vibewarden.yaml JSON equivalents pass schema validation.
func TestConfigSchemaAcceptsValidDocuments(t *testing.T) {
	sch := compileConfigSchema(t)

	tests := []struct {
		name string
		json string
	}{
		{
			name: "minimal empty document",
			json: `{}`,
		},
		{
			name: "dev profile with server and upstream",
			json: `{
				"profile": "dev",
				"server": {"host": "127.0.0.1", "port": 8443},
				"upstream": {"host": "127.0.0.1", "port": 3000}
			}`,
		},
		{
			name: "tls with letsencrypt",
			json: `{
				"tls": {
					"enabled": true,
					"provider": "letsencrypt",
					"domain": "example.com",
					"storage_path": "./data/caddy"
				}
			}`,
		},
		{
			name: "tls with self-signed",
			json: `{
				"tls": {"enabled": true, "provider": "self-signed"}
			}`,
		},
		{
			name: "tls with external provider",
			json: `{
				"tls": {
					"enabled": true,
					"provider": "external",
					"cert_path": "/etc/certs/tls.crt",
					"key_path": "/etc/certs/tls.key"
				}
			}`,
		},
		{
			name: "auth kratos mode",
			json: `{
				"auth": {
					"enabled": true,
					"mode": "kratos",
					"public_paths": ["/health", "/ready"],
					"session_cookie_name": "ory_kratos_session"
				}
			}`,
		},
		{
			name: "auth jwt mode",
			json: `{
				"auth": {
					"enabled": true,
					"mode": "jwt",
					"jwt": {
						"jwks_url": "https://idp.example.com/.well-known/jwks.json",
						"issuer": "https://idp.example.com/",
						"audience": "my-api",
						"allowed_algorithms": ["RS256"],
						"cache_ttl": "1h"
					}
				}
			}`,
		},
		{
			name: "auth api-key mode with static keys",
			json: `{
				"auth": {
					"enabled": true,
					"mode": "api-key",
					"api_key": {
						"header": "X-API-Key",
						"keys": [
							{"name": "ci-deploy", "hash": "abc123def456", "scopes": ["deploy"]}
						],
						"scope_rules": [
							{"path": "/admin/*", "required_scopes": ["admin"]}
						]
					}
				}
			}`,
		},
		{
			name: "auth with social providers",
			json: `{
				"auth": {
					"mode": "kratos",
					"social_providers": [
						{
							"provider": "github",
							"client_id": "gh-client-id",
							"client_secret": "gh-secret"
						},
						{
							"provider": "oidc",
							"client_id": "oidc-client-id",
							"client_secret": "oidc-secret",
							"id": "acme-oidc",
							"issuer_url": "https://accounts.example.com"
						}
					]
				}
			}`,
		},
		{
			name: "auth ui built-in",
			json: `{
				"auth": {
					"ui": {
						"mode": "built-in",
						"app_name": "My App",
						"primary_color": "#7C3AED",
						"background_color": "#1a1a2e"
					}
				}
			}`,
		},
		{
			name: "auth ui custom",
			json: `{
				"auth": {
					"ui": {
						"mode": "custom",
						"login_url": "https://app.example.com/login"
					}
				}
			}`,
		},
		{
			name: "rate limit with memory store",
			json: `{
				"rate_limit": {
					"enabled": true,
					"store": "memory",
					"per_ip": {"requests_per_second": 10, "burst": 20},
					"per_user": {"requests_per_second": 100, "burst": 200},
					"exempt_paths": ["/health"]
				}
			}`,
		},
		{
			name: "rate limit with redis store",
			json: `{
				"rate_limit": {
					"enabled": true,
					"store": "redis",
					"redis": {
						"address": "localhost:6379",
						"password": "secret",
						"db": 0,
						"key_prefix": "vibewarden",
						"fallback": true,
						"health_check_interval": "30s"
					}
				}
			}`,
		},
		{
			name: "rate limit with redis URL",
			json: `{
				"rate_limit": {
					"store": "redis",
					"redis": {
						"url": "redis://:password@redis.example.com:6379/0"
					}
				}
			}`,
		},
		{
			name: "security headers with CSP struct",
			json: `{
				"security_headers": {
					"enabled": true,
					"hsts_max_age": 31536000,
					"frame_option": "DENY",
					"csp": {
						"default_src": ["'self'"],
						"script_src": ["'self'", "https://cdn.example.com"],
						"img_src": ["'self'", "data:"]
					}
				}
			}`,
		},
		{
			name: "telemetry with prometheus and otlp",
			json: `{
				"telemetry": {
					"enabled": true,
					"path_patterns": ["/users/:id"],
					"prometheus": {"enabled": true},
					"otlp": {
						"enabled": true,
						"endpoint": "http://collector:4318",
						"interval": "30s",
						"protocol": "http"
					},
					"logs": {"otlp": true},
					"traces": {"enabled": true}
				}
			}`,
		},
		{
			name: "database with pool and external URL",
			json: `{
				"database": {
					"external_url": "postgres://user:pass@db.example.com:5432/kratos?sslmode=require",
					"tls_mode": "verify-full",
					"pool": {"max_conns": 20, "min_conns": 5},
					"connect_timeout": "15s"
				}
			}`,
		},
		{
			name: "body size with overrides",
			json: `{
				"body_size": {
					"max": "10MB",
					"overrides": [
						{"path": "/api/upload", "max": "100MB"}
					]
				}
			}`,
		},
		{
			name: "ip filter allowlist",
			json: `{
				"ip_filter": {
					"enabled": true,
					"mode": "allowlist",
					"addresses": ["10.0.0.0/8", "192.168.1.100"],
					"trust_proxy_headers": false
				}
			}`,
		},
		{
			name: "webhooks endpoints",
			json: `{
				"webhooks": {
					"endpoints": [
						{
							"url": "https://hooks.example.com/vibewarden",
							"events": ["auth.failed", "rate_limit.hit"],
							"format": "slack",
							"timeout_seconds": 10
						}
					]
				}
			}`,
		},
		{
			name: "secrets with openbao",
			json: `{
				"secrets": {
					"enabled": true,
					"provider": "openbao",
					"openbao": {
						"address": "http://openbao:8200",
						"auth": {"method": "approle", "role_id": "role-abc", "secret_id": "secret-xyz"},
						"mount_path": "secret"
					},
					"inject": {
						"headers": [
							{"secret_path": "db/creds", "secret_key": "password", "header": "X-DB-Password"}
						]
					},
					"cache_ttl": "5m"
				}
			}`,
		},
		{
			name: "resilience with circuit breaker and retry",
			json: `{
				"resilience": {
					"timeout": "30s",
					"circuit_breaker": {
						"enabled": true,
						"threshold": 5,
						"timeout": "60s"
					},
					"retry": {
						"enabled": true,
						"max_attempts": 3,
						"backoff": "100ms",
						"max_backoff": "10s",
						"retry_on": [502, 503, 504]
					}
				}
			}`,
		},
		{
			name: "cors config",
			json: `{
				"cors": {
					"enabled": true,
					"allowed_origins": ["https://app.example.com"],
					"allowed_methods": ["GET", "POST", "PUT", "DELETE", "OPTIONS"],
					"allowed_headers": ["Content-Type", "Authorization"],
					"allow_credentials": true,
					"max_age": 600
				}
			}`,
		},
		{
			name: "observability stack",
			json: `{
				"observability": {
					"enabled": true,
					"grafana_port": 3001,
					"prometheus_port": 9090,
					"loki_port": 3100,
					"retention_days": 14
				}
			}`,
		},
		{
			name: "audit config",
			json: `{
				"audit": {
					"enabled": true,
					"output": "/var/log/vibewarden/audit.jsonl"
				}
			}`,
		},
		{
			name: "waf with all rule categories",
			json: `{
				"waf": {
					"enabled": true,
					"mode": "block",
					"rules": {
						"sqli": true,
						"xss": true,
						"path_traversal": true,
						"command_injection": true
					},
					"exempt_paths": ["/api/raw"],
					"content_type_validation": {
						"enabled": true,
						"allowed": ["application/json", "multipart/form-data"]
					}
				}
			}`,
		},
		{
			name: "input validation with path overrides",
			json: `{
				"input_validation": {
					"enabled": true,
					"max_url_length": 4096,
					"max_query_string_length": 2048,
					"max_header_count": 50,
					"max_header_size": 8192,
					"path_overrides": [
						{"path": "/api/search", "max_query_string_length": 8192}
					]
				}
			}`,
		},
		{
			name: "egress proxy with routes",
			json: `{
				"egress": {
					"enabled": true,
					"listen": "127.0.0.1:8081",
					"default_policy": "deny",
					"allow_insecure": false,
					"default_timeout": "30s",
					"dns": {"block_private": true},
					"routes": [
						{
							"name": "stripe",
							"pattern": "https://api.stripe.com/**",
							"methods": ["POST"],
							"timeout": "15s",
							"secret": "stripe/api-key",
							"secret_header": "Authorization",
							"secret_format": "Bearer {value}",
							"circuit_breaker": {"threshold": 3, "reset_after": "30s"},
							"retries": {"max": 2, "backoff": "exponential"},
							"validate_response": {
								"status_codes": ["2xx"],
								"content_types": ["application/json"]
							},
							"headers": {
								"add": {"X-Source": "vibewarden"},
								"remove_response": ["X-Powered-By"]
							},
							"sanitize": {
								"headers": ["Authorization"],
								"query_params": ["api_key"],
								"body_fields": ["password"]
							}
						}
					]
				}
			}`,
		},
		{
			name: "error pages",
			json: `{
				"error_pages": {
					"enabled": true,
					"directory": "/etc/vibewarden/error-pages"
				}
			}`,
		},
		{
			name: "maintenance mode",
			json: `{
				"maintenance": {
					"enabled": true,
					"message": "We will be back shortly."
				}
			}`,
		},
		{
			name: "compression",
			json: `{
				"compression": {
					"enabled": true,
					"algorithms": ["zstd", "gzip"]
				}
			}`,
		},
		{
			name: "response headers modification",
			json: `{
				"response_headers": {
					"set": {"X-Custom-Header": "value"},
					"add": {"X-Extra": "extra"},
					"remove": ["X-Powered-By", "Server"]
				}
			}`,
		},
		{
			name: "watch config",
			json: `{
				"watch": {
					"enabled": true,
					"debounce": "500ms"
				}
			}`,
		},
		{
			name: "kratos with smtp and external mode",
			json: `{
				"kratos": {
					"public_url": "https://kratos.example.com",
					"admin_url": "https://kratos-admin.example.com",
					"external": true,
					"smtp": {
						"host": "smtp.example.com",
						"port": 587,
						"from": "no-reply@example.com"
					}
				}
			}`,
		},
		{
			name: "overrides",
			json: `{
				"overrides": {
					"kratos_config": "/etc/vibewarden/kratos.yml",
					"identity_schema": "/etc/vibewarden/identity.schema.json"
				}
			}`,
		},
		{
			name: "admin api",
			json: `{
				"admin": {
					"enabled": true,
					"token": "super-secret-token"
				}
			}`,
		},
		{
			name: "app config with build context",
			json: `{
				"app": {
					"build": ".",
					"healthcheck": "curl -sf http://localhost:3000/health || exit 1"
				}
			}`,
		},
		{
			name: "upstream with health checker",
			json: `{
				"upstream": {
					"host": "127.0.0.1",
					"port": 3000,
					"health": {
						"enabled": true,
						"path": "/health",
						"interval": "10s",
						"timeout": "5s",
						"unhealthy_threshold": 3,
						"healthy_threshold": 2
					}
				}
			}`,
		},
		{
			name: "server with custom timeouts",
			json: `{
				"server": {
					"host": "0.0.0.0",
					"port": 443,
					"read_timeout": "30s",
					"write_timeout": "60s",
					"idle_timeout": "120s"
				}
			}`,
		},
		{
			name: "tls cert monitoring",
			json: `{
				"tls": {
					"enabled": true,
					"provider": "letsencrypt",
					"domain": "example.com",
					"cert_monitoring": {
						"enabled": true,
						"check_interval": "6h",
						"warning_threshold": "720h",
						"critical_threshold": "168h"
					}
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := unmarshalJSON(t, tt.json)
			if err := sch.Validate(inst); err != nil {
				raw, _ := json.MarshalIndent(inst, "", "  ")
				t.Errorf("unexpected schema validation failure:\n%v\ndocument:\n%s", err, raw)
			}
		})
	}
}

// TestConfigSchemaRejectsInvalidDocuments verifies that the schema correctly
// rejects documents that violate structural constraints.
func TestConfigSchemaRejectsInvalidDocuments(t *testing.T) {
	sch := compileConfigSchema(t)

	tests := []struct {
		name string
		json string
	}{
		{
			name: "unknown top-level property",
			json: `{"unknown_field": "value"}`,
		},
		{
			name: "invalid profile value",
			json: `{"profile": "staging"}`,
		},
		{
			name: "server port below minimum",
			json: `{"server": {"port": 0}}`,
		},
		{
			name: "server port above maximum",
			json: `{"server": {"port": 65536}}`,
		},
		{
			name: "invalid tls provider",
			json: `{"tls": {"provider": "cloudflare"}}`,
		},
		{
			name: "invalid auth mode",
			json: `{"auth": {"mode": "oauth2"}}`,
		},
		{
			name: "api key entry missing required name",
			json: `{"auth": {"api_key": {"keys": [{"hash": "abc123"}]}}}`,
		},
		{
			name: "api key entry missing required hash",
			json: `{"auth": {"api_key": {"keys": [{"name": "ci"}]}}}`,
		},
		{
			name: "invalid social provider",
			json: `{"auth": {"social_providers": [{"provider": "twitter", "client_id": "id", "client_secret": "secret"}]}}`,
		},
		{
			name: "social provider missing required client_id",
			json: `{"auth": {"social_providers": [{"provider": "github", "client_secret": "secret"}]}}`,
		},
		{
			name: "invalid auth ui mode",
			json: `{"auth": {"ui": {"mode": "headless"}}}`,
		},
		{
			name: "invalid primary color — not a hex color",
			json: `{"auth": {"ui": {"primary_color": "purple"}}}`,
		},
		{
			name: "invalid rate_limit store",
			json: `{"rate_limit": {"store": "memcache"}}`,
		},
		{
			name: "invalid log level",
			json: `{"log": {"level": "trace"}}`,
		},
		{
			name: "invalid log format",
			json: `{"log": {"format": "yaml"}}`,
		},
		{
			name: "webhook endpoint missing required url",
			json: `{"webhooks": {"endpoints": [{"events": ["auth.failed"]}]}}`,
		},
		{
			name: "webhook endpoint missing required events",
			json: `{"webhooks": {"endpoints": [{"url": "https://hooks.example.com"}]}}`,
		},
		{
			name: "webhook endpoint empty events array",
			json: `{"webhooks": {"endpoints": [{"url": "https://hooks.example.com", "events": []}]}}`,
		},
		{
			name: "invalid webhook format",
			json: `{"webhooks": {"endpoints": [{"url": "https://h.example.com", "events": ["*"], "format": "teams"}]}}`,
		},
		{
			name: "invalid database tls_mode",
			json: `{"database": {"tls_mode": "allow"}}`,
		},
		{
			name: "invalid ip_filter mode",
			json: `{"ip_filter": {"mode": "whitelist"}}`,
		},
		{
			name: "invalid egress default_policy",
			json: `{"egress": {"default_policy": "passthrough"}}`,
		},
		{
			name: "egress route missing required name",
			json: `{"egress": {"routes": [{"pattern": "https://api.example.com/**"}]}}`,
		},
		{
			name: "egress route missing required pattern",
			json: `{"egress": {"routes": [{"name": "api"}]}}`,
		},
		{
			name: "invalid egress retry backoff strategy",
			json: `{"egress": {"routes": [{"name": "r", "pattern": "https://a.com/**", "retries": {"backoff": "linear"}}]}}`,
		},
		{
			name: "body_size override missing required path",
			json: `{"body_size": {"overrides": [{"max": "10MB"}]}}`,
		},
		{
			name: "input_validation path_override missing required path",
			json: `{"input_validation": {"path_overrides": [{"max_url_length": 1024}]}}`,
		},
		{
			name: "invalid waf mode",
			json: `{"waf": {"mode": "passive"}}`,
		},
		{
			name: "invalid otlp protocol",
			json: `{"telemetry": {"otlp": {"protocol": "tcp"}}}`,
		},
		{
			name: "upstream health threshold below minimum",
			json: `{"upstream": {"health": {"unhealthy_threshold": 0}}}`,
		},
		{
			name: "rate limit redis negative db index",
			json: `{"rate_limit": {"redis": {"db": -1}}}`,
		},
		{
			name: "cors max_age below minimum",
			json: `{"cors": {"max_age": -1}}`,
		},
		{
			name: "compression unknown algorithm",
			json: `{"compression": {"algorithms": ["brotli"]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := unmarshalJSON(t, tt.json)
			if err := sch.Validate(inst); err == nil {
				t.Errorf("expected schema validation to fail for %q, but it passed", tt.name)
			}
		})
	}
}

// TestConfigSchemaAcceptsAllProfiles ensures every valid profile value passes.
func TestConfigSchemaAcceptsAllProfiles(t *testing.T) {
	sch := compileConfigSchema(t)

	for _, profile := range []string{"dev", "tls", "prod"} {
		t.Run(fmt.Sprintf("profile=%s", profile), func(t *testing.T) {
			inst := unmarshalJSON(t, fmt.Sprintf(`{"profile": %q}`, profile))
			if err := sch.Validate(inst); err != nil {
				t.Errorf("profile %q should be valid but got: %v", profile, err)
			}
		})
	}
}
