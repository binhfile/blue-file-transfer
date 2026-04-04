BINARY_NAME=bft
VERSION=0.1.0
LDFLAGS=-ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build linux linux-arm64 windows all test test-short test-integration bench coverage check clean dist dist-linux dist-linux-arm64

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

# Package release tarballs (binary + install scripts)
dist-linux: linux
	tar czf $(BINARY_NAME)-linux-amd64.tar.gz \
		-C . $(BINARY_NAME)-linux-amd64 \
		-C . scripts/install-server.sh scripts/install-web.sh \
		README.md
	@echo "Created $(BINARY_NAME)-linux-amd64.tar.gz"

dist-linux-arm64: linux-arm64
	tar czf $(BINARY_NAME)-linux-arm64.tar.gz \
		-C . $(BINARY_NAME)-linux-arm64 \
		-C . scripts/install-server.sh scripts/install-web.sh \
		README.md
	@echo "Created $(BINARY_NAME)-linux-arm64.tar.gz"

dist: dist-linux dist-linux-arm64
	@echo "All release tarballs created"

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux-amd64 $(BINARY_NAME)-windows-amd64.exe $(BINARY_NAME)-linux-arm64
	rm -f $(BINARY_NAME)-linux-amd64.tar.gz $(BINARY_NAME)-linux-arm64.tar.gz
	rm -f coverage.out coverage.html
