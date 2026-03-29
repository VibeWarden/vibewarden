"""Minimal Flask application demonstrating VibeWarden sidecar integration.

The app exposes /health, /public, and /protected endpoints.
Authentication is handled by VibeWarden — the app reads user identity from
the X-User-* request headers forwarded by the sidecar.
"""

import os
from datetime import datetime, timezone

from flask import Flask, jsonify, request

app = Flask(__name__)
PORT = int(os.environ.get("PORT", 5000))


@app.get("/health")
def health():
    """Liveness probe — always public."""
    return jsonify({"status": "ok"})


@app.get("/public")
def public():
    """Public endpoint — no authentication required."""
    return jsonify(
        {
            "message": "This is a public endpoint",
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }
    )


@app.get("/protected")
def protected():
    """Protected endpoint.

    VibeWarden verifies the request before it reaches here.
    User identity is forwarded via X-User-* headers.
    """
    user_id = request.headers.get("X-User-Id", "unknown")
    user_email = request.headers.get("X-User-Email", "unknown")

    return jsonify(
        {
            "message": "You reached a protected endpoint",
            "user_id": user_id,
            "user_email": user_email,
        }
    )


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=PORT)
