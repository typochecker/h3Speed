# h3Speed

A comprehensive HTTP3 speed testing tool that can test upload and download performance with multiple connections and streams.

## Features

- **Multi-protocol support**: Unix domain socket, HTTP, and HTTP3
- **Real-time speed monitoring**: Updates every second during tests
- **Configurable testing**: Support for multiple connections and streams per connection
- **Random data generation**: Generates random data for upload/download tests
- **Command-line interface**: Easy to use with flexible parameters

## Architecture

This tool is designed to work with nginx as a reverse proxy:
```
Client (quic-go) -> Nginx (HTTP3) -> Server (HTTP)
```

The server can be deployed behind nginx and accessed through different URL paths.

## Building

```bash
# Build both server and client
make build

# Build individually
make server  # builds bin/h3speed-server
make client  # builds bin/h3speed-client

# Static build (fully static, CGO disabled)
make build-static           # builds bin/h3speed-*-static for current platform

# Cross-compile static binaries (examples)
GOOS=linux GOARCH=amd64 make build-static
GOOS=linux GOARCH=arm64 make build-static
```

## Usage

### Server

Start the server (supports all three protocols simultaneously):

```bash
./bin/h3speed-server
```

The server will listen on:
- Unix socket: `/tmp/h3speed.sock`
- HTTP: `:8080`
- HTTP3: `:8443`

### Client

The client supports the following parameters:

- `-mode`: `upload` or `download` (default: `download`)
- `-url`: Server URL (default: `https://localhost:8443`)
- `-time`: Test duration (default: `30s`)
- `-connections`: Number of connections (default: `1`)
- `-streams`: Number of streams per connection (default: `1`)

#### Examples

**Basic download test:**
```bash
./bin/h3speed-client -mode=download -url=https://localhost:8443 -time=10s
```

**Upload test with multiple connections and streams:**
```bash
./bin/h3speed-client -mode=upload -url=https://localhost:8443 -time=30s -connections=4 -streams=3
```

**HTTP test (non-HTTP3):**
```bash
./bin/h3speed-client -mode=download -url=http://localhost:8080 -time=10s -connections=2 -streams=1
```

### Sample Output

```
Starting download test to https://localhost:8443 for 5s with 2 connections and 2 streams per connection
Current: 123.6 MB/s | Average: 123.6 MB/s | Total: 123.6 MB
Current: 132.2 MB/s | Average: 127.9 MB/s | Total: 255.8 MB
Current: 122.1 MB/s | Average: 125.9 MB/s | Total: 377.9 MB
Current: 118.0 MB/s | Average: 124.0 MB/s | Total: 495.9 MB
Current: 143.8 MB/s | Average: 127.9 MB/s | Total: 639.7 MB

=== Final Statistics ===
Total time: 5.000455329s
Total bytes: 639.7 MB
Average speed: 127.9 MB/s
```

### Stream Behavior

**Download Mode**: Multiple streams share a single HTTP request/response. This means that increasing the number of streams will NOT increase total throughput - all streams will read from the same data source. This behavior correctly simulates scenarios where you want to test how well an application handles concurrent reads from the same data stream.

**Upload Mode**: Multiple streams create separate HTTP requests. This allows testing upload capacity with concurrent uploads, which can increase total throughput.

Example:
```bash
# These commands will show similar download speeds
./bin/h3speed-client -mode=download -streams=1  # ~40 MB/s
./bin/h3speed-client -mode=download -streams=4  # ~40 MB/s (shared)

# These commands will show different upload speeds  
./bin/h3speed-client -mode=upload -streams=1    # ~45 MB/s
./bin/h3speed-client -mode=upload -streams=4    # ~180 MB/s (concurrent)
```

## API Endpoints

The server provides the following endpoints:

- `GET /health` - Health check endpoint
- `POST /upload` - Upload endpoint (accepts any data, returns bytes received)
- `GET /download` - Download endpoint (streams random data)

## Nginx Configuration Example

```nginx
location /h3speed/ {
    proxy_pass http://localhost:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Then use the client with: `-url=https://yourdomain.com/h3speed`

## Development

```bash
# Format code
make fmt

# Clean build artifacts
make clean

# Install dependencies
make deps

# Show help
make help
```
