# H3Speed Makefile

.PHONY: all build clean server client test \
	build-static server-static client-static help fmt deps bin

# Configurable variables (can be overridden at invocation time)
# Example: make build-static GOOS=linux GOARCH=arm64
GOOS ?=
GOARCH ?=

# Static build flags
STATIC_TAGS ?= osusergo,netgo
STATIC_LDFLAGS ?= -s -w -extldflags "-static"

# Build both server and client
all: build

# Build server and client binaries
build: server client

# Build the server
server: bin
	@echo "Building server..."
	@go build -o bin/h3speed-server ./cmd/server

# Build the client
client: bin
	@echo "Building client..."
	@go build -o bin/h3speed-client ./cmd/client

# Static build (fully static, CGO disabled)
build-static: server-static client-static

server-static: bin
	@echo "Building static server..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -tags '$(STATIC_TAGS)' -ldflags '$(STATIC_LDFLAGS)' \
		-o bin/h3speed-server-static ./cmd/server

client-static: bin
	@echo "Building static client..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -tags '$(STATIC_TAGS)' -ldflags '$(STATIC_LDFLAGS)' \
		-o bin/h3speed-client-static ./cmd/client

# Run the server
run-server: server
	@echo "Starting server..."
	@./bin/h3speed-server

# Example client runs
run-client-download: client
	@echo "Running download test..."
	@./bin/h3speed-client -mode=download -url=https://localhost:8443 -time=10s -connections=2 -streams=2

run-client-upload: client
	@echo "Running upload test..."
	@./bin/h3speed-client -mode=upload -url=https://localhost:8443 -time=10s -connections=2 -streams=2

# Test HTTP endpoint
test-http: client
	@echo "Testing HTTP endpoint..."
	@./bin/h3speed-client -mode=download -url=http://localhost:8080 -time=5s -connections=1 -streams=1

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/

# Create bin directory
bin:
	@mkdir -p bin

# Format code
fmt:
	@go fmt ./...

# Run tests
test:
	@go test ./...

# Install dependencies
deps:
	@go mod tidy

# Help
help:
	@echo "Available targets:"
	@echo "  all           - Build both server and client"
	@echo "  build         - Build both server and client"
	@echo "  server        - Build server only"
	@echo "  client        - Build client only"
	@echo "  build-static  - Build statically linked server and client (CGO disabled)"
	@echo "  server-static - Build statically linked server only (outputs bin/h3speed-server-static)"
	@echo "  client-static - Build statically linked client only (outputs bin/h3speed-client-static)"
	@echo "  run-server    - Build and run server"
	@echo "  run-client-download - Run client in download mode"
	@echo "  run-client-upload   - Run client in upload mode"
	@echo "  test-http     - Test HTTP endpoint"
	@echo "  clean         - Remove build artifacts"
	@echo "  fmt           - Format code"
	@echo "  test          - Run tests"
	@echo "  deps          - Install/update dependencies"
	@echo "  help          - Show this help"