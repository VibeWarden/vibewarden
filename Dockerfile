# Dockerfile — customize for your app
#
# Example for a Go app:
#   FROM golang:1.24-alpine AS build
#   WORKDIR /app
#   COPY . .
#   RUN go build -o app ./cmd/nodescapp
#   FROM alpine:3.21
#   COPY --from=build /app/app /app
#   EXPOSE 3000
#   CMD ["/app"]
#
# Example for a Node.js app:
#   FROM node:22-alpine
#   WORKDIR /app
#   COPY package*.json ./
#   RUN npm ci
#   COPY . .
#   EXPOSE 3000
#   CMD ["node", "index.js"]
#
# Example for a Python app:
#   FROM python:3.13-slim
#   WORKDIR /app
#   COPY requirements.txt .
#   RUN pip install --no-cache-dir -r requirements.txt
#   COPY . .
#   EXPOSE 3000
#   CMD ["python", "app.py"]

# Replace this with your actual Dockerfile
