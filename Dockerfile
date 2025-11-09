# Multi-stage build for minimal image size
FROM golang:1.21-bookworm AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source
COPY *.go ./

# Build with ARM support
ARG GOARCH=arm
ARG GOARM=7
RUN CGO_ENABLED=1 GOOS=linux GOARCH=$GOARCH GOARM=$GOARM \
    go build -ldflags="-s -w" -o pi-dashcam .

# Runtime image
FROM debian:bookworm-slim

# Install minimal dependencies
RUN apt-get update && apt-get install -y \
    libcamera0 \
    libcamera-tools \
    libraspberrypi-bin \
    libraspberrypi0 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary
COPY --from=builder /app/pi-dashcam .

# Create directories
RUN mkdir -p /var/lib/pi-dashcam/videos /etc/pi-dashcam

# Expose port
EXPOSE 8080

# Run
ENTRYPOINT ["/app/pi-dashcam"]
CMD ["-config", "/etc/pi-dashcam/config.json", "-v"]
