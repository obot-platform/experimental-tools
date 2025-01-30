package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type KVResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type KVRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type OutputFilterRequest struct {
	Output       string `json:"output"`
	Chat         bool   `json:"chat,omitempty"`
	Continuation bool   `json:"continuation,omitempty"`
}

type OutputFilterResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Key     string `json:"key,omitempty"`
}

type Server struct {
	nc *nats.Conn
}

// getGPTScriptEnv extracts environment values from the X-GPTScript-Env header
func getGPTScriptEnv(headers http.Header, envKey string) string {
	// Use CanonicalHeaderKey to handle case-insensitive header names
	envHeaders := headers[http.CanonicalHeaderKey("X-Gptscript-Env")]

	for _, env := range envHeaders {
		log.Printf("Processing env header: %s", env)
		pairs := strings.Split(env, ",")
		for _, pair := range pairs {
			kv := strings.Split(pair, "=")
			if len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				value := strings.TrimSpace(kv[1])
				log.Printf("Checking key-value pair: %s = %s", key, value)
				if key == envKey {
					return value
				}
			}
		}
	}

	return ""
}

// getPrefixFromEnv generates a SHA1 prefix from the environment value
func getPrefixFromEnv(headers http.Header) string {
	envValue := getGPTScriptEnv(headers, "GPTSCRIPT_WORKSPACE_ID")
	if envValue == "" {
		log.Printf("Warning: No GPTSCRIPT_WORKSPACE_ID found in headers")
		return "default"
	}

	hasher := sha1.New()
	hasher.Write([]byte(envValue))
	prefix := hex.EncodeToString(hasher.Sum(nil))
	log.Printf("Using bucket prefix: %s (from GPTSCRIPT_WORKSPACE_ID: %s)", prefix, envValue)
	return prefix
}

// getFullKey converts a user key to a full internal key path
func getFullKey(headers http.Header, userKey string) string {
	prefix := getPrefixFromEnv(headers)
	log.Printf("Prefix: %s", prefix)
	return "/" + prefix + "/" + userKey
}

// getUserKey converts an internal full key path to a user-visible key
func getUserKey(fullKey string) string {
	parts := strings.Split(fullKey, "/")
	if len(parts) >= 3 {
		return parts[2] // Return just the UUID part
	}
	return fullKey // Fallback
}

func NewServer(nc *nats.Conn) (*Server, error) {
	return &Server{
		nc: nc,
	}, nil
}

// getBucket gets or creates a bucket for the given prefix
func (s *Server) getBucket(prefix string) (nats.KeyValue, error) {
	js, err := s.nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %v", err)
	}

	kv, err := js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket: prefix,
	})
	if err != nil {
		// If it already exists, try to get it
		kv, err = js.KeyValue(prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create/get KV store: %v", err)
		}
	}
	return kv, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Create a response wrapper to capture the response
	rw := &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
		body:           new(bytes.Buffer),
	}
	w = rw

	// Log incoming request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Error reading request", http.StatusInternalServerError)
		return
	}
	// Replace the body for further processing
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// Log request details including headers
	log.Printf("Request: %s %s", r.Method, r.URL.Path)
	log.Printf("Headers:")
	for name, values := range r.Header {
		for _, value := range values {
			log.Printf("  %s: %s", name, value)
		}
	}
	if len(body) > 0 {
		log.Printf("Request Body: %s", string(body))
	}

	w.Header().Set("Content-Type", "application/json")

	// Handle health check endpoint
	if r.URL.Path == "/api/ready" && r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		log.Printf("Response: %d", http.StatusOK)
		return
	}

	// All other endpoints should be POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		log.Printf("Response: %d - Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle KV operations
	switch r.URL.Path {
	case "/api/v1/get":
		s.handleGet(w, r)
	case "/api/v1/put":
		s.handlePut(w, r)
	case "/api/v1/delete":
		s.handleDelete(w, r)
	case "/api/v1/list":
		s.handleList(w, r)
	case "/api/v1/output-filter":
		s.handleOutputFilter(w, r)
	default:
		http.NotFound(w, r)
		log.Printf("Response: 404 - Not Found")
		return
	}

	// Log response
	log.Printf("Response Status: %d", rw.status)
	log.Printf("Response Body: %s", rw.body.String())
}

// responseWriter is a wrapper for http.ResponseWriter that captures the status code and response body
type responseWriter struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	var req KVRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: "invalid request body"})
		return
	}

	if req.Key == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: "key is required"})
		return
	}

	// Get the bucket for this request
	prefix := getPrefixFromEnv(r.Header)
	bucket, err := s.getBucket(prefix)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	entry, err := bucket.Get(req.Key)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(KVResponse{Success: true, Data: string(entry.Value())})
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	var req KVRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	if req.Key == "" || req.Value == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: "key and value are required"})
		return
	}

	// Get the bucket for this request
	prefix := getPrefixFromEnv(r.Header)
	bucket, err := s.getBucket(prefix)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	_, err = bucket.Put(req.Key, []byte(req.Value))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(KVResponse{Success: true})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req KVRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: "invalid request body"})
		return
	}

	if req.Key == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: "key is required"})
		return
	}

	// Get the bucket for this request
	prefix := getPrefixFromEnv(r.Header)
	bucket, err := s.getBucket(prefix)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	err = bucket.Delete(req.Key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(KVResponse{Success: true})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	// Get the bucket for this request
	prefix := getPrefixFromEnv(r.Header)
	bucket, err := s.getBucket(prefix)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	keys, err := bucket.ListKeys()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(KVResponse{Success: false, Error: err.Error()})
		return
	}

	keyList := make([]string, 0)
	for k := range keys.Keys() {
		keyList = append(keyList, k)
	}

	json.NewEncoder(w).Encode(KVResponse{Success: true, Data: keyList})
}

func (s *Server) handleOutputFilter(w http.ResponseWriter, r *http.Request) {
	var req OutputFilterRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(OutputFilterResponse{Success: false, Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	// Get tool name from header
	toolName := r.Header.Get("X-Gptscript-Tool-Name")
	if toolName == "" {
		toolName = "unknown"
	}

	// Generate a new SHA1 for uniqueness
	hasher := sha1.New()
	hasher.Write([]byte(uuid.New().String()))
	uniqueHash := hex.EncodeToString(hasher.Sum(nil))

	// Create the key in format: output-<tool>-<sha1>
	key := fmt.Sprintf("output-%s-%s", toolName, uniqueHash)
	log.Printf("Generated output key: %s", key)

	// Get the bucket for this request
	prefix := getPrefixFromEnv(r.Header)
	bucket, err := s.getBucket(prefix)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(OutputFilterResponse{Success: false, Error: err.Error()})
		return
	}

	// Store just the output as the value
	_, err = bucket.Put(key, []byte(req.Output))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(OutputFilterResponse{Success: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(OutputFilterResponse{
		Success: true,
		Key:     key,
	})
}

func main() {
	// Get PORT from environment and calculate NATS port
	port := getEnvOrDefault("PORT", "8080")
	portInt := 0
	if _, err := fmt.Sscanf(port, "%d", &portInt); err != nil {
		log.Fatalf("Invalid PORT number: %v", err)
	}
	natsPort := portInt + 1

	// Get default values from environment variables
	defaultAddr := getEnvOrDefault("NATS_HOST", "0.0.0.0")
	defaultStorage := getEnvOrDefault("NATS_STORAGE", "./data")

	// Define command line flags with shorter names
	addr := flag.String("h", defaultAddr, "Address to listen on (env: NATS_HOST)")
	storageDir := flag.String("s", defaultStorage, "Directory for storing data (env: NATS_STORAGE)")
	flag.Parse()

	// Ensure storage directory exists
	if err := os.MkdirAll(*storageDir, 0755); err != nil {
		log.Fatalf("Failed to create storage directory: %v", err)
	}

	// Configure NATS server options
	opts := &server.Options{
		Host:      *addr,
		Port:      natsPort,
		JetStream: true,
		StoreDir:  filepath.Clean(*storageDir),
		NoLog:     false,
		NoSigs:    true,
	}

	// Create and start the NATS server
	ns, err := server.NewServer(opts)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Configure server logging
	ns.ConfigureLogger()

	// Start the server
	go ns.Start()

	if !ns.ReadyForConnections(4 * 1e9) { // Wait up to 4 seconds for server to be ready
		log.Fatal("Failed to start server")
	}

	// Connect to NATS
	nc, err := nats.Connect(fmt.Sprintf("nats://%s:%d", *addr, natsPort))
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Create and configure the HTTP server
	httpServer, err := NewServer(nc)
	if err != nil {
		log.Fatalf("Failed to create HTTP server: %v", err)
	}

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on port %s", port)
		if err := http.ListenAndServe(":"+port, httpServer); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Printf("NATS Server is running on %s:%d", *addr, natsPort)
	log.Printf("Storage directory: %s", *storageDir)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	fmt.Println("\nShutting down servers...")
	ns.Shutdown()
	ns.WaitForShutdown()
}

func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
