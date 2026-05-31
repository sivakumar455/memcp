.PHONY: build build-all clean test run fmt vet

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.Version=$(VERSION)

# Build memcp (upstream, no site extensions)
build:
	@echo "Building memcp $(VERSION)..."
	@mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o bin/memcp ./cmd/memcp
	@echo "Built: bin/memcp"

# Build with site extensions (company watchers, etc.)
build-site:
	@echo "Building memcp $(VERSION) with site extensions..."
	@mkdir -p bin
	go build -tags site -ldflags="$(LDFLAGS)" -o bin/memcp ./cmd/memcp
	@echo "Built: bin/memcp (site)"

# Aliases
all: build
	@echo "All binaries built in bin/"

build-mcp: build

# Run tests
test:
	go test ./... -v -count=1

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf data/
	rm -rf tmp/

# Run memcp (standalone mode)
run: build
	bin/memcp

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@mkdir -p bin
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-darwin-arm64 ./cmd/memcp
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-darwin-amd64 ./cmd/memcp
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-linux-arm64 ./cmd/memcp
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-linux-amd64 ./cmd/memcp
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-windows-amd64.exe ./cmd/memcp
	@echo "Done. Binaries in bin/"

# Format code
fmt:
	go fmt ./...

# Lint
vet:
	go vet ./...
