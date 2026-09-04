# VibeWarden Node.js / Express example — Dockerfile
#
# Multi-stage build: install production dependencies in a build stage,
# copy only the required files into the final image.

# ---------------------------------------------------------------------------
# Stage 1: install dependencies
# ---------------------------------------------------------------------------
FROM node:26-alpine AS deps

WORKDIR /app

COPY package.json package-lock.json* ./
# `npm install`, not `npm ci`: this example deliberately ships no lockfile (see
# the /examples/node-express entry in .github/dependabot.yml), and `npm ci`
# hard-fails with EUSAGE when there is none — the `package-lock.json*` glob
# above silently copies nothing. This build never succeeded (#1495).
RUN npm install --omit=dev --no-audit --no-fund

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM node:26-alpine

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY index.js package.json ./

# The app listens on port 3000 by default.
EXPOSE 3000

CMD ["node", "index.js"]
