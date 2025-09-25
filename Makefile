# H3Speed Makefile

.PHONY: all build clean server client test

# Build both server and client
all: build

# Build server and client binaries
build: server client

# Build the server
server:
	@echo "Building server..."
	@go build -o bin/h3speed-server ./cmd/server

# Build the client
client:
	@echo "Building client..."
	@go build -o bin/h3speed-client ./cmd/client

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
	@echo "  run-server    - Build and run server"
	@echo "  run-client-download - Run client in download mode"
	@echo "  run-client-upload   - Run client in upload mode"
	@echo "  test-http     - Test HTTP endpoint"
	@echo "  clean         - Remove build artifacts"
	@echo "  fmt           - Format code"
	@echo "  test          - Run tests"
	@echo "  deps          - Install/update dependencies"
	@echo "  help          - Show this help"