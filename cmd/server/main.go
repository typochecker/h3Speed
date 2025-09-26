package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/http3"
)

const (
	bufferSize = 64 * 1024 // 64KB buffer
)

type Server struct {
	httpServer   *http.Server
	http3Server  *http3.Server
	unixServer   *http.Server
	unixListener net.Listener

	// Configurable addresses/paths
	httpAddr     string
	http3Addr    string
	unixSockPath string
}

func main() {
	// Flags for configurable ports/paths
	httpAddr := flag.String("http", ":8080", "HTTP listen address (host:port)")
	h3Addr := flag.String("h3", ":8443", "HTTP/3 listen address (host:port)")
	unixSock := flag.String("unix", "/tmp/h3speed.sock", "Unix domain socket path")
	flag.Parse()

	server := &Server{
		httpAddr:     *httpAddr,
		http3Addr:    *h3Addr,
		unixSockPath: *unixSock,
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-signalChan
		log.Println("Shutting down servers...")
		cancel()
	}()

	// Start all servers
	server.startServers(ctx)
}

func (s *Server) startServers(ctx context.Context) {
	// Setup HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/download", s.handleDownload)
	mux.HandleFunc("/health", s.handleHealth)

	// Start Unix domain socket server
	go s.startUnixServer(ctx, mux)

	// Start HTTP server
	go s.startHTTPServer(ctx, mux)

	// Start HTTP3 server
	go s.startHTTP3Server(ctx, mux)

	log.Println("All servers started successfully!")
	log.Printf("Unix socket: %s", s.unixSockPath)
	log.Printf("HTTP: %s", s.httpAddr)
	log.Printf("HTTP3: %s", s.http3Addr)

	<-ctx.Done()
	s.shutdown()
}

func (s *Server) startUnixServer(ctx context.Context, mux *http.ServeMux) {
	sockPath := s.unixSockPath

	// Remove existing socket file
	os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		log.Fatalf("Failed to create Unix socket: %v", err)
	}

	s.unixListener = listener
	s.unixServer = &http.Server{Handler: mux}
	s.unixServer.ConnState = func(c net.Conn, state http.ConnState) {
		ra := c.RemoteAddr().String()
		switch state {
		case http.StateNew:
			log.Printf("[UNIX] conn new from %s", ra)
		case http.StateActive:
			log.Printf("[UNIX] conn active %s", ra)
		case http.StateIdle:
			log.Printf("[UNIX] conn idle %s", ra)
		case http.StateHijacked:
			log.Printf("[UNIX] conn hijacked %s", ra)
		case http.StateClosed:
			log.Printf("[UNIX] conn closed %s", ra)
		}
	}

	log.Printf("Unix socket server listening on %s", sockPath)

	if err := s.unixServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Printf("Unix server error: %v", err)
	}
}

func (s *Server) startHTTPServer(ctx context.Context, mux *http.ServeMux) {
	s.httpServer = &http.Server{Addr: s.httpAddr, Handler: mux}
	s.httpServer.ConnState = func(c net.Conn, state http.ConnState) {
		ra := c.RemoteAddr().String()
		switch state {
		case http.StateNew:
			log.Printf("[HTTP] conn new from %s", ra)
		case http.StateActive:
			log.Printf("[HTTP] conn active %s", ra)
		case http.StateIdle:
			log.Printf("[HTTP] conn idle %s", ra)
		case http.StateHijacked:
			log.Printf("[HTTP] conn hijacked %s", ra)
		case http.StateClosed:
			log.Printf("[HTTP] conn closed %s", ra)
		}
	}

	log.Printf("HTTP server listening on %s", s.httpAddr)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}

func (s *Server) startHTTP3Server(ctx context.Context, mux *http.ServeMux) {
	// Generate self-signed certificate for testing
	cert, err := generateSelfSignedCert()
	if err != nil {
		log.Fatalf("Failed to generate certificate: %v", err)
	}

	s.http3Server = &http3.Server{
		Addr:    s.http3Addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}

	log.Printf("HTTP3 server listening on %s", s.http3Addr)

	if err := s.http3Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP3 server error: %v", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	log.Printf("[REQ] %s %s from=%s xff=%s proto=%s", r.Method, r.URL.Path, r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Proto)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	log.Printf("[REQ] %s %s from=%s xff=%s proto=%s", r.Method, r.URL.Path, r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Proto)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and discard all data
	buffer := make([]byte, bufferSize)
	totalBytes := int64(0)

	for {
		n, err := r.Body.Read(buffer)
		totalBytes += int64(n)

		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "Error reading data", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Received %d bytes", totalBytes)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	log.Printf("[REQ] %s %s from=%s xff=%s proto=%s", r.Method, r.URL.Path, r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Proto)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate and send random data
	w.Header().Set("Content-Type", "application/octet-stream")

	buffer := make([]byte, bufferSize)

	// Keep sending data until client disconnects
	for {
		// Generate random data
		if _, err := rand.Read(buffer); err != nil {
			log.Printf("Error generating random data: %v", err)
			return
		}

		// Send data
		if _, err := w.Write(buffer); err != nil {
			// Client disconnected
			log.Printf("[REQ] download writer error, likely client closed: %v", err)
			return
		}

		// Flush to ensure data is sent immediately
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Small delay to prevent overwhelming the connection
		time.Sleep(time.Millisecond)
	}
}

func (s *Server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.httpServer != nil {
		s.httpServer.Shutdown(ctx)
	}

	if s.http3Server != nil {
		s.http3Server.Close()
	}

	if s.unixServer != nil && s.unixListener != nil {
		s.unixServer.Shutdown(ctx)
		s.unixListener.Close()
		os.Remove("/tmp/h3speed.sock")
	}

	log.Println("All servers shut down")
}

func generateSelfSignedCert() (tls.Certificate, error) {
	// Generate a private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"H3Speed Test"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"Test"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:    []string{"localhost"},
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Encode certificate to PEM format
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM format
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyDER,
	})

	// Create TLS certificate
	return tls.X509KeyPair(certPEM, privateKeyPEM)
}
