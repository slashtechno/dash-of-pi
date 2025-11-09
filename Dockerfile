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
RUN go build -ldflags="-s" -o pi-dashcam .

# Runtime image
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/pi-dashcam .

# Create directories and expose port
RUN mkdir -p /var/lib/pi-dashcam/videos /etc/pi-dashcam

EXPOSE 8080

ENTRYPOINT ["/app/pi-dashcam"]
CMD ["-config", "/etc/pi-dashcam/config.json",