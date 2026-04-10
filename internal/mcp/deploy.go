package mcp

import (
	"fmt"
	"strings"
)

// SidecarImage is the canonical VibeWarden sidecar Docker image reference.
const SidecarImage = "ghcr.io/vibewarden/vibewarden:latest"

// Platform represents a supported deployment platform.
type Platform string

const (
	// PlatformDocker is a generic Docker Compose deployment used with vibew deploy SSH.
	PlatformDocker Platform = "docker"
	// PlatformRailway is a Railway.app two-service deployment using internal networking.
	PlatformRailway Platform = "railway"
	// PlatformFlyio is a Fly.io deployment using the processes syntax in fly.toml.
	PlatformFlyio Platform = "flyio"
)

// SidecarSpec describes the VibeWarden sidecar container in a deployment.
type SidecarSpec struct {
	// Image is the container image reference.
	Image string `json:"image"`
	// Ports lists the host:container port mappings (Docker syntax).
	Ports []string `json:"ports"`
	// Environment lists the required environment variable names (values are placeholders).
	Environment map[string]string `json:"environment"`
	// Volumes lists the host:container volume mounts.
	Volumes []string `json:"volumes"`
	// Command is the optional override command.
	Command []string `json:"command,omitempty"`
}

// DeploySpec is the full deployment specification returned by vibewarden_prepare_deploy.
type DeploySpec struct {
	// Platform identifies which platform this spec targets.
	Platform string `json:"platform"`
	// AppPort is the port the user's application listens on.
	AppPort int `json:"app_port"`
	// Sidecar is the container spec for the VibeWarden sidecar.
	Sidecar SidecarSpec `json:"sidecar"`
	// ConfigFileContent is the ready-to-use vibewarden.yaml template.
	ConfigFileContent string `json:"config_file_content"`
	// PlatformSpec is a platform-native configuration snippet (Compose YAML,
	// Railway JSON, or fly.toml TOML depending on the platform).
	PlatformSpec string `json:"platform_spec"`
	// HealthCheckURL is the URL agents should poll to verify deployment.
	HealthCheckURL string `json:"health_check_url"`
	// Notes contains platform-specific hints and next steps.
	Notes []string `json:"notes"`
}

// PrepareDockerSpec builds a generic Docker Compose deployment spec.
func PrepareDockerSpec(appPort int) DeploySpec {
	configContent := buildVibewardenYAML(appPort)

	compose := fmt.Sprintf(`services:
  app:
    image: your-app-image:latest   # replace with your actual image or build context
    expose:
      - "%d"
    networks:
      - vibeward

  vibewarden:
    image: %s
    ports:
      - "8443:8443"
    volumes:
      - ./vibewarden.yaml:/etc/vibewarden/vibewarden.yaml:ro
      - vibewarden_data:/data
    environment:
      VIBEWARDEN_CONFIG: /etc/vibewarden/vibewarden.yaml
    depends_on:
      - app
    networks:
      - vibeward
    restart: unless-stopped

networks:
  vibeward:
    driver: bridge

volumes:
  vibewarden_data:
`, appPort, SidecarImage)

	return DeploySpec{
		Platform: string(PlatformDocker),
		AppPort:  appPort,
		Sidecar: SidecarSpec{
			Image: SidecarImage,
			Ports: []string{"8443:8443"},
			Environment: map[string]string{
				"VIBEWARDEN_CONFIG": "/etc/vibewarden/vibewarden.yaml",
			},
			Volumes: []string{
				"./vibewarden.yaml:/etc/vibewarden/vibewarden.yaml:ro",
				"vibewarden_data:/data",
			},
		},
		ConfigFileContent: configContent,
		PlatformSpec:      compose,
		HealthCheckURL:    "https://localhost:8443/_vibewarden/health",
		Notes: []string{
			"Replace 'your-app-image:latest' with your actual application image or add a 'build:' key.",
			"The sidecar listens on port 8443 (HTTPS) and proxies to your app on port " + fmt.Sprintf("%d", appPort) + " within the 'vibeward' network.",
			"Run 'vibew deploy ssh' to push this Compose file to a remote server.",
			"Set VIBEWARDEN_ADMIN_TOKEN in your environment or .env file before starting.",
		},
	}
}

// PrepareRailwaySpec builds a Railway.app two-service deployment spec.
// The sidecar and the app communicate over Railway's private internal network.
func PrepareRailwaySpec(appPort int) DeploySpec {
	configContent := buildVibewardenYAML(appPort)

	// railway.toml for the sidecar service.
	railwaySpec := fmt.Sprintf(`# railway.toml — place this in the vibewarden service root

[build]
builder = "DOCKERFILE"
dockerfilePath = ""         # not needed; image is pulled directly

[deploy]
startCommand = "vibewarden serve --config /etc/vibewarden/vibewarden.yaml"

# Add these environment variables in the Railway dashboard:
#   VIBEWARDEN_CONFIG       = /etc/vibewarden/vibewarden.yaml
#   VIBEWARDEN_ADMIN_TOKEN  = <generated secret>
#   APP_INTERNAL_HOST       = <your-app-service>.railway.internal

# Railway networking notes:
#   - Services on the same project communicate via <service-name>.railway.internal
#   - The sidecar upstream.host must be set to your app's internal hostname
#   - Public traffic hits the sidecar; the app service has no public port

# vibewarden.yaml upstream section for Railway:
# upstream:
#   host: your-app-name.railway.internal
#   port: %d
`, appPort)

	return DeploySpec{
		Platform: string(PlatformRailway),
		AppPort:  appPort,
		Sidecar: SidecarSpec{
			Image: SidecarImage,
			Ports: []string{"8443:8443"},
			Environment: map[string]string{
				"VIBEWARDEN_CONFIG":      "/etc/vibewarden/vibewarden.yaml",
				"VIBEWARDEN_ADMIN_TOKEN": "<generate with: vibew secret generate --admin-token>",
			},
			Volumes: []string{},
		},
		ConfigFileContent: configContent,
		PlatformSpec:      railwaySpec,
		HealthCheckURL:    "https://<your-vibewarden-railway-domain>/_vibewarden/health",
		Notes: []string{
			"Deploy the VibeWarden sidecar as a separate Railway service in the same project as your app.",
			"Set upstream.host in vibewarden.yaml to your app's Railway internal hostname: <app-name>.railway.internal",
			"Only the vibewarden service needs a public domain; keep your app service internal.",
			"Store VIBEWARDEN_ADMIN_TOKEN as a Railway secret variable.",
			"Add vibewarden.yaml as a Railway volume or embed it in a custom Docker image.",
		},
	}
}

// PrepareFlyioSpec builds a Fly.io deployment spec using the processes syntax.
// The sidecar runs as a separate process group in the same Fly app, sharing the private network.
func PrepareFlyioSpec(appPort int) DeploySpec {
	configContent := buildVibewardenYAML(appPort)

	flyToml := fmt.Sprintf(`# fly.toml — Fly.io deployment with VibeWarden sidecar process
# Place this in your project root and run: fly deploy

app = "your-app-name"        # replace with your Fly app name
primary_region = "iad"       # replace with your preferred region

# ── App process ──────────────────────────────────────────────────────────────
[processes]
  app        = "./your-app-binary"            # replace with your app start command
  vibewarden = "vibewarden serve --config /etc/vibewarden/vibewarden.yaml"

# ── Services (public ingress via vibewarden) ─────────────────────────────────
[[services]]
  processes   = ["vibewarden"]
  internal_port = 8443
  protocol      = "tcp"

  [[services.ports]]
    port     = 443
    handlers = ["tls", "http"]

  [[services.ports]]
    port     = 80
    handlers = ["http"]

  [services.concurrency]
    type       = "requests"
    hard_limit = 200
    soft_limit = 150

# ── App internal service (not exposed publicly) ───────────────────────────────
[[services]]
  processes     = ["app"]
  internal_port = %d
  protocol      = "tcp"
  # No public ports — only the sidecar is exposed

# ── Mounts ────────────────────────────────────────────────────────────────────
[[mounts]]
  source      = "vibewarden_data"
  destination = "/data"
  processes   = ["vibewarden"]

# ── Environment ───────────────────────────────────────────────────────────────
[env]
  VIBEWARDEN_CONFIG = "/etc/vibewarden/vibewarden.yaml"
  # Set VIBEWARDEN_ADMIN_TOKEN as a secret:
  #   fly secrets set VIBEWARDEN_ADMIN_TOKEN=<value>
`, appPort)

	return DeploySpec{
		Platform: string(PlatformFlyio),
		AppPort:  appPort,
		Sidecar: SidecarSpec{
			Image: SidecarImage,
			Ports: []string{"443:8443"},
			Environment: map[string]string{
				"VIBEWARDEN_CONFIG": "/etc/vibewarden/vibewarden.yaml",
			},
			Volumes: []string{
				"vibewarden_data:/data",
			},
			Command: []string{"vibewarden", "serve", "--config", "/etc/vibewarden/vibewarden.yaml"},
		},
		ConfigFileContent: configContent,
		PlatformSpec:      flyToml,
		HealthCheckURL:    "https://<your-app-name>.fly.dev/_vibewarden/health",
		Notes: []string{
			"The 'vibewarden' process in [processes] runs the sidecar; the 'app' process runs your application.",
			"Set the upstream.host in vibewarden.yaml to '0.0.0.0' (localhost within the VM) since both processes share the same Fly machine.",
			"Run 'fly secrets set VIBEWARDEN_ADMIN_TOKEN=<value>' to store the admin token securely.",
			"Mount vibewarden.yaml using a Fly volume or bake it into a custom Dockerfile that uses " + SidecarImage + " as base.",
			"Run 'fly deploy' to deploy. Check health with: fly ssh console -C 'curl http://localhost:8443/_vibewarden/health'",
		},
	}
}

// buildVibewardenYAML generates a minimal vibewarden.yaml template for the given app port.
func buildVibewardenYAML(appPort int) string {
	var sb strings.Builder
	sb.WriteString("# vibewarden.yaml — generated by vibewarden_prepare_deploy\n")
	sb.WriteString("# Review and adjust before deploying.\n\n")
	sb.WriteString("server:\n  port: 8443\n\n")
	fmt.Fprintf(&sb, "upstream:\n  port: %d\n\n", appPort)
	sb.WriteString("tls:\n  enabled: true\n  provider: self-signed  # change to letsencrypt with a real domain\n\n")
	sb.WriteString("log:\n  level: info\n  format: json\n\n")
	sb.WriteString("security_headers:\n  enabled: true\n\n")
	sb.WriteString("admin:\n  enabled: false  # set to true and provide admin.token to enable user management\n")
	return sb.String()
}
