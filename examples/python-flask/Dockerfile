# VibeWarden Python / Flask example — Dockerfile
#
# Multi-stage build: install dependencies in a build stage,
# copy only the required files into the final image.

# ---------------------------------------------------------------------------
# Stage 1: install dependencies
# ---------------------------------------------------------------------------
FROM python:3.13-slim AS deps

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM python:3.13-slim

WORKDIR /app

COPY --from=deps /install /usr/local
COPY app.py ./

# The app listens on port 5000 by default.
EXPOSE 5000

CMD ["python", "app.py"]
