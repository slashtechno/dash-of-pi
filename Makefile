.PHONY: help build build-arm clean test run run-docker docker-build docker-up docker-down logs install uninstall

BINARY_NAME=pi-dashcam
VERSION?=dev
GOARCH?=amd64
GOARM?=7

help:
	@echo "Pi DashCam - Build and Development Commands"
	@echo ""
	@echo "Build:"
	@echo "  make build              Build for current system"
	@echo "  make build-arm          Cross-compile for ARM (Pi Zero 2W)"
	@echo "  make clean              Remove build artifacts"
	@echo ""
	@echo "Development:"
	@echo "  make run                Run locally (requires camera access)"
	@echo "  make test               Run tests"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build       Build Docker image"
	@echo "  make docker-up          Start with docker-compose"
	@echo "  make docker-down        Stop docker-compose"
	@echo "  make logs               Follow docker logs"
	@echo ""
	@echo "Systemd:"
	@echo "  make install            Install as systemd service (requires sudo)"
	@echo "  make uninstall          Remove systemd service (requires sudo)"

build:
	@echo "Building $(BINARY_NAME) for $(GOARCH)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BINARY_NAME) .
	@echo "Binary: $(BINARY_NAME)"

build-arm:
	@echo "Cross-compiling for ARM (GOARCH=arm, GOARM=7)..."
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o $(BINARY_NAME)-arm .
	@echo "Binary: $(BINARY_NAME)-arm"

clean:
	@echo "Cleaning up..."
	rm -f $(BINARY_NAME) $(BINARY_NAME)-arm
	rm -rf dist/ build/
	@echo "Clean complete"

test:
	@echo "Running tests..."
	go test -v ./...

run: build
	@echo "Running $(BINARY_NAME) locally..."
	./$(BINARY_NAME) -config ./config.json -v

docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):latest .

docker-up:
	@echo "Starting docker-compose..."
	docker-compose up -d
	@echo "Dashboard: http://localhost:8080"

docker-down:
	@echo "Stopping docker-compose..."
	docker-compose down

logs:
	docker-compose logs -f $(BINARY_NAME)

install:
	@echo "Installing systemd service..."
	sudo ./scripts/install.sh

uninstall:
	@echo "Uninstalling systemd service..."
	sudo ./scripts/uninstall.sh

# Development helpers
fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

deps:
	go mod tidy
	go mod verify

pi-ip:
	@echo "Raspberry Pi is likely at:"
	@ping -c 1 raspberrypi.local 2>/dev/null && echo "http://raspberrypi.local:8080" || echo "Use 'hostname -I' on the Pi to find its IP"
