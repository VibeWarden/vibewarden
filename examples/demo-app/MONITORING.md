# Uptime Monitoring for the VibeWarden Public Demo

This document describes how to set up external uptime monitoring for the
public demo infrastructure at `challenge.vibewarden.dev` and
`dashboard.vibewarden.dev`.

Recommended tools (both have a free tier adequate for this purpose):

- **UptimeRobot** — https://uptimerobot.com — free tier: up to 50 monitors,
  5-minute check interval, email and webhook alerts.
- **Hetrixtools** — https://hetrixtools.com — free tier: up to 15 monitors,
  3-minute check interval, email alerts.

---

## Monitored endpoints

| Monitor | URL | Expected response |
|---|---|---|
| Primary health check | `https://challenge.vibewarden.dev/health` | HTTP 200, body `{"status":"ok"}` |
| Grafana dashboard | `https://dashboard.vibewarden.dev` | HTTP 200 |

The `/health` endpoint is the authoritative liveness signal for the
VibeWarden sidecar. It is excluded from authentication and rate limiting,
so a failure there means the sidecar itself is down or unreachable.

The Grafana check confirms the observability stack (Prometheus, Loki,
Grafana) is up. A failure here does not mean the sidecar is broken, but it
does mean the demo dashboard is unavailable.

---

## UptimeRobot — step-by-step setup

### Prerequisites

1. Create a free account at https://uptimerobot.com.
2. Verify your email address.

### Monitor 1 — Primary health check

1. In the UptimeRobot dashboard, click **+ Add New Monitor**.
2. Fill in the form:
   - **Monitor Type**: `HTTPS`
   - **Friendly Name**: `VibeWarden Demo — Health`
   - **URL**: `https://challenge.vibewarden.dev/health`
   - **Monitoring Interval**: `Every 5 minutes`
3. Expand **Advanced Settings**:
   - Under **Keyword Monitoring**, enable **Alert if keyword not found** and
     enter `"status":"ok"` as the keyword. This ensures the monitor fails
     if the endpoint returns 200 but with a broken body.
4. Under **Alert Contacts**, add your email address (and/or a Discord
   webhook — see below).
5. Click **Create Monitor**.

### Monitor 2 — Grafana dashboard

1. Click **+ Add New Monitor** again.
2. Fill in the form:
   - **Monitor Type**: `HTTPS`
   - **Friendly Name**: `VibeWarden Demo — Grafana`
   - **URL**: `https://dashboard.vibewarden.dev`
   - **Monitoring Interval**: `Every 5 minutes`
3. Under **Alert Contacts**, add the same contacts as above.
4. Click **Create Monitor**.

### Adding a Discord webhook alert

1. In your Discord server, open **Server Settings > Integrations > Webhooks**.
2. Click **New Webhook**, give it a name (e.g. `VibeWarden Uptime`), choose
   the channel, then click **Copy Webhook URL**.
3. In UptimeRobot, go to **My Settings > Alert Contacts > Add Alert Contact**.
4. Set:
   - **Alert Contact Type**: `Discord`
   - **Friendly Name**: `Discord — vibewarden alerts`
   - **Webhook URL**: paste the URL you copied
5. Click **Create Alert Contact**.
6. Edit each monitor and add this new contact under **Alert Contacts**.

UptimeRobot will now post a message to your Discord channel when a monitor
goes down and again when it recovers.

---

## Hetrixtools — quick setup (alternative)

1. Create a free account at https://hetrixtools.com.
2. Navigate to **Uptime Monitors > Add Monitor**.
3. For the primary health check:
   - **Type**: `Website`
   - **URL**: `https://challenge.vibewarden.dev/health`
   - **Check interval**: `3 minutes`
   - **Keyword check**: enable and enter `"status":"ok"`
4. Repeat for `https://dashboard.vibewarden.dev` without a keyword check.
5. Configure email or webhook notifications under **Notification Lists**.

---

## Verifying the health endpoint locally

Before configuring external monitoring, confirm the endpoint behaves as
expected:

```bash
curl -s https://challenge.vibewarden.dev/health
# Expected output: {"status":"ok"}
```

If the sidecar is running locally you can verify the same endpoint on port
8080:

```bash
curl -s http://localhost:8080/health
# Expected output: {"status":"ok"}
```

---

## Alert runbook

When UptimeRobot (or Hetrixtools) fires a down alert:

1. SSH into the Hetzner demo VM.
2. Check container status:
   ```bash
   docker compose -f /opt/vibewarden-demo/docker-compose.prod.yml ps
   ```
3. Check VibeWarden logs:
   ```bash
   docker compose -f /opt/vibewarden-demo/docker-compose.prod.yml logs --tail=100 vibewarden
   ```
4. If the container is stopped, restart it:
   ```bash
   docker compose -f /opt/vibewarden-demo/docker-compose.prod.yml up -d vibewarden
   ```
5. Re-check the health endpoint:
   ```bash
   curl -s https://challenge.vibewarden.dev/health
   ```
6. If the issue persists, check Caddy TLS renewal logs and Kratos status.
