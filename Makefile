.PHONY: build clean test run

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY := bin/memcp
LDFLAGS := -s -w -X main.Version=$(VERSION)

# Build the binary
build:
	@echo "Building memcp $(VERSION)..."
	@mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .
	@echo "Built: $(BINARY)"

# Run tests
test:
	go test ./... -v -count=1

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf data/
	rm -rf tmp/

# Run the server (standalone mode)
run: build
	$(BINARY)

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@mkdir -p bin
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-darwin-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-linux-arm64 .
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-linux-amd64 .
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/memcp-windows-amd64.exe .
	@echo "Done. Binaries in bin/"

# Format code
fmt:
	go fmt ./...

# Lint
vet:
	go vet ./...
