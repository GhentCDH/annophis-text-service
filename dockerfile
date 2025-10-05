# --- Build stage ---
FROM golang:1.25-alpine AS build
WORKDIR /src

# For VCS-backed modules
RUN apk add --no-cache git

# Better caching for deps
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/annophis-text-service ./cmd/annophis-text-service

# --- Runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
WORKDIR /app

# Copy binary
COPY --from=build /out/annophis-text-service /app/annophis-text-service

# Default config (can be overridden with a bind mount or ENV CONFIG)
COPY config.json /app/config.json

# Run as non-root
RUN adduser -D -u 10001 appuser
USER appuser

EXPOSE 8080

# Simple healthcheck
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENV ORIGIN_ALLOWED=""
ENV CONFIG=/app/config.json

ENTRYPOINT ["/app/annophis-text-service"]
