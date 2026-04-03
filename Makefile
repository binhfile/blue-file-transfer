BINARY_NAME=bft
VERSION=0.1.0
LDFLAGS=-ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build linux windows test test-integration bench coverage clean

# Build for current platform
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/bft/

# Build for Linux amd64
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 ./cmd/bft/
# Build for Linux ard64
linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 /usr/local/go/bin/go build $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 ./cmd/bft/
# Build for Windows amd64
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe ./cmd/bft/

# Build all platforms
all: linux windows

# Run all unit tests
test:
	go test -v -count=1 -race ./internal/...

# Run unit tests with short flag (skip integration)
test-short:
	go test -v -short -count=1 -race ./internal/...

# Run integration tests (requires 2 BT adapters)
test-integration:
	go test -v -count=1 -run Integration ./internal/...

# Run benchmarks
bench:
	go test -bench=. -benchmem ./internal/...

# Generate coverage report
coverage:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Vet and staticcheck
check:
	go vet ./...

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux-amd64 $(BINARY_NAME)-windows-amd64.exe
	rm -f coverage.out coverage.html
