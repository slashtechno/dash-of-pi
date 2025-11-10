# Multi-stage build for minimal image size
# Builds for the current system (native compilation - no cross-compilation)
FROM golang:1.21-bookworm AS builder

WORKDIR /app

# Copy go mod files and download deps
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY *.go ./

# Build binary for current system
RUN go build -ldflags="-s" -o dash-of-pi .

# Runtime image
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/dash-of-pi .

# Copy web assets
COPY web/ ./web/

# Create directories and expose port
RUN mkdir -p /var/lib/dash-of-pi/videos /etc/dash-of-pi

EXPOSE 8080

ENTRYPOINT ["/app/dash-of-pi"]
CMD ["-config", "/etc/dash-of-pi/config.json"]