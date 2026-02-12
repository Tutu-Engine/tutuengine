# ──────────────────────────────────────────────────────────────────────────────
# TuTu Network — Production Dockerfile
# Multi-stage build: Compile Go binary → minimal runtime image
# ──────────────────────────────────────────────────────────────────────────────

# Stage 1: Build
FROM golang:1.23-bookworm AS builder

WORKDIR /src

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags "-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo docker)" \
  -o /app/tutu ./cmd/tutu

# Stage 2: Runtime (distroless for minimal attack surface)
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/tutu /app/tutu

# TuTu data directory (Railway manages volumes separately)
ENV TUTU_HOME=/data

# API server port
EXPOSE 11434

# Health check endpoint
# Note: distroless doesn't have curl; Railway uses HTTP healthcheck
ENTRYPOINT ["/app/tutu"]
CMD ["serve"]
