# VibeWarden Next.js example — Dockerfile
#
# Multi-stage build leveraging Next.js standalone output mode.
# next.config.js sets output: 'standalone' so the runtime image
# only needs the files copied from .next/standalone.

# ---------------------------------------------------------------------------
# Stage 1: install dependencies
# ---------------------------------------------------------------------------
FROM node:22-alpine AS deps

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci

# ---------------------------------------------------------------------------
# Stage 2: build
# ---------------------------------------------------------------------------
FROM node:22-alpine AS builder

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

RUN npm run build

# ---------------------------------------------------------------------------
# Stage 3: runtime
# ---------------------------------------------------------------------------
FROM node:22-alpine

WORKDIR /app

ENV NODE_ENV=production

COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public* ./public/

# The app listens on port 3000 by default.
EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
