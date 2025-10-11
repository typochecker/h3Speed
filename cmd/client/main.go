package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

type Config struct {
	Mode        string // "upload" or "download"
	URL         string
	Duration    time.Duration
	Connections int
	Streams     int
	H3Idle      time.Duration
}

type SpeedMonitor struct {
	bytesTransferred int64
	startTime        time.Time
	mutex            sync.RWMutex
	lastBytes        int64
	lastTime         time.Time
}

func main() {
	var config Config

	flag.StringVar(&config.Mode, "mode", "download", "Mode: upload or download")
	flag.StringVar(&config.URL, "url", "https://localhost:8443", "Server URL")
	flag.DurationVar(&config.Duration, "time", 30*time.Second, "Test duration")
	flag.IntVar(&config.Connections, "connections", 1, "Number of connections")
	flag.IntVar(&config.Streams, "streams", 1, "Number of streams per connection")
	flag.DurationVar(&config.H3Idle, "h3-idle", 5*time.Second, "HTTP/3 QUIC max idle timeout")
	flag.Parse()

	if config.Mode != "upload" && config.Mode != "download" {
		log.Fatal("Mode must be either 'upload' or 'download'")
	}

	fmt.Printf("Starting %s test to %s for %v with %d connections and %d streams per connection\n",
		config.Mode, config.URL, config.Duration, config.Connections, config.Streams)

	monitor := &SpeedMonitor{
		startTime: time.Now(),
		lastTime:  time.Now(),
	}

	// Handle Ctrl+C (SIGINT) / SIGTERM to cancel gracefully
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start speed monitoring with time limit AND signal cancellation
	ctx, cancel := context.WithTimeout(sigCtx, config.Duration)
	defer cancel()

	go monitor.displaySpeed(ctx)

	// Build a shared HTTP client (reused by all connections)
	var (
		client      *http.Client
		h3Transport *http3.Transport
		h1Transport *http.Transport
	)

	if strings.HasPrefix(config.URL, "https://") {
		// Use HTTP/3 transport
		h3Transport = &http3.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			QUICConfig: &quic.Config{
				MaxIdleTimeout: config.H3Idle,
			},
		}
		client = &http.Client{Transport: h3Transport}
	} else {
		// Use standard HTTP/1.1 transport (and allow HTTPS without HTTP/3)
		h1Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: h1Transport}
	}

	// On signal cancellation, aggressively close transports to abort in-flight requests
	go func() {
		<-sigCtx.Done()
		if h3Transport != nil {
			_ = h3Transport.Close()
		}
		if h1Transport != nil {
			h1Transport.CloseIdleConnections()
		}
	}()

	// Start test workers
	var wg sync.WaitGroup

	for i := 0; i < config.Connections; i++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()
			runConnection(ctx, config, client, monitor, connID)
		}(i)
	}

	wg.Wait()

	// Proactively close transports (especially HTTP/3) on exit
	log.Println("Shutting down client: closing transports...")
	if h3Transport != nil {
		_ = h3Transport.Close()
	}
	if h1Transport != nil {
		h1Transport.CloseIdleConnections()
	}
	// Give a tiny grace period to allow CONNECTION_CLOSE to be sent
	time.Sleep(200 * time.Millisecond)

	// Final statistics
	monitor.printFinalStats()
}

func (sm *SpeedMonitor) addBytes(bytes int64) {
	atomic.AddInt64(&sm.bytesTransferred, bytes)
}

func (sm *SpeedMonitor) displaySpeed(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.printCurrentSpeed()
		}
	}
}

func (sm *SpeedMonitor) printCurrentSpeed() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	now := time.Now()
	currentBytes := atomic.LoadInt64(&sm.bytesTransferred)

	timeDiff := now.Sub(sm.lastTime).Seconds()
	bytesDiff := currentBytes - sm.lastBytes

	if timeDiff > 0 {
		currentSpeed := float64(bytesDiff) / timeDiff
		totalSpeed := float64(currentBytes) / now.Sub(sm.startTime).Seconds()

		fmt.Printf("Current: %s/s | Average: %s/s | Total: %s\n",
			formatBytes(int64(currentSpeed)),
			formatBytes(int64(totalSpeed)),
			formatBytes(currentBytes))
	}

	sm.lastBytes = currentBytes
	sm.lastTime = now
}

func (sm *SpeedMonitor) printFinalStats() {
	totalTime := time.Since(sm.startTime)
	totalBytes := atomic.LoadInt64(&sm.bytesTransferred)
	avgSpeed := float64(totalBytes) / totalTime.Seconds()

	fmt.Printf("\n=== Final Statistics ===\n")
	fmt.Printf("Total time: %v\n", totalTime)
	fmt.Printf("Total bytes: %s\n", formatBytes(totalBytes))
	fmt.Printf("Average speed: %s/s\n", formatBytes(int64(avgSpeed)))
}

func runConnection(ctx context.Context, config Config, client *http.Client, monitor *SpeedMonitor, connID int) {
	// Both upload and download modes now use multiple concurrent requests
	var wg sync.WaitGroup

	for i := 0; i < config.Streams; i++ {
		wg.Add(1)
		go func(streamID int) {
			defer wg.Done()
			runStream(ctx, config, client, monitor, connID, streamID)
		}(i)
	}

	wg.Wait()
}

func runStream(ctx context.Context, config Config, client *http.Client, monitor *SpeedMonitor, connID, streamID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if config.Mode == "upload" {
				doUpload(ctx, config, client, monitor)
			} else {
				doDownload(ctx, config, client, monitor)
			}
		}
	}
}

func doUpload(ctx context.Context, config Config, client *http.Client, monitor *SpeedMonitor) {
	// Generate random data to upload
	data := make([]byte, 64*1024) // 64KB chunks
	if _, err := rand.Read(data); err != nil {
		log.Printf("Error generating random data: %v", err)
		return
	}

	url := config.URL + "/upload"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // Context cancelled
		}
		log.Printf("Error uploading: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		monitor.addBytes(int64(len(data)))
	}

	// Read response body to completion
	io.Copy(io.Discard, resp.Body)
}

func doDownload(ctx context.Context, config Config, client *http.Client, monitor *SpeedMonitor) {
	url := config.URL + "/download"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // Context cancelled
		}
		log.Printf("Error downloading: %v", err)
		return
	}
	defer resp.Body.Close()

	// Ensure ctx cancellation unblocks any pending Body.Read by closing the body
	go func() {
		<-ctx.Done()
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Server returned status: %d", resp.StatusCode)
		return
	}

	// Read data in chunks
	buffer := make([]byte, 64*1024)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, err := resp.Body.Read(buffer)
			if n > 0 {
				monitor.addBytes(int64(n))
			}

			if err == io.EOF {
				return
			}
			if err != nil {
				if ctx.Err() != nil {
					return // Context cancelled
				}
				log.Printf("Error reading response: %v", err)
				return
			}
		}
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
