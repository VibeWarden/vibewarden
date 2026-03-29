# VibeWarden Node.js / Express example — Dockerfile
#
# Multi-stage build: install production dependencies in a build stage,
# copy only the required files into the final image.

# ---------------------------------------------------------------------------
# Stage 1: install dependencies
# ---------------------------------------------------------------------------
FROM node:22-alpine AS deps

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --omit=dev

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM node:22-alpine

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY index.js package.json ./

# The app listens on port 3000 by default.
EXPOSE 3000

CMD ["node", "index.js"]
