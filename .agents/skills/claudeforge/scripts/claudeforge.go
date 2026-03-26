package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultPort      = "7890"
	targetBaseURL    = "https://api.anthropic.com"
	logDir           = ".agents/logs/claudeforge"
	requestsLogFile  = "requests.log"
	responsesLogFile = "responses.log"
)

var (
	port string
)

func init() {
	flag.StringVar(&port, "port", defaultPort, "Port to listen on")
}

var client = newClient()

type requestLog struct {
	Timestamp string            `json:"timestamp"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Query     string            `json:"query"`
	Headers   map[string]string `json:"headers"`
	Body      json.RawMessage   `json:"body,omitempty"`
}

type responseLog struct {
	Timestamp string            `json:"timestamp"`
	Status    int               `json:"status"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body,omitempty"`
}

func main() {
	flag.Parse()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	log.Printf("ClaudeForge proxy starting on localhost:%s", port)
	log.Printf("Forwarding to %s", targetBaseURL)
	log.Printf("Logs will be written to ./%s/", logDir)
	log.Printf("Use: ANTHROPIC_BASE_URL=http://localhost:%s claude -p ...", port)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", handleMessages)

	server := &http.Server{
		Addr:    "localhost:" + port,
		Handler: mux,
	}

	log.Fatalf("Server error: %v", server.ListenAndServe())
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	reqID := timestamp
	logEntry := fmt.Sprintf("=== CLAUDEFORGE REQUEST [%s] ===", reqID)
	log.Println(logEntry)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}

	reqLog := requestLog{
		Timestamp: timestamp,
		Method:    r.Method,
		Path:      "/v1/messages",
		Query:     r.URL.RawQuery,
		Headers:   headers,
	}

	if len(bodyBytes) > 0 {
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err == nil {
			reqLog.Body = prettyJSON.Bytes()
		} else {
			reqLog.Body = bodyBytes
		}
	}

	reqLogJSON, _ := json.MarshalIndent(reqLog, "", "  ")
	log.Println(string(reqLogJSON))

	saveLog(filepath.Join(logDir, fmt.Sprintf("request_%s.json", strings.ReplaceAll(timestamp, ":", "-"))), reqLogJSON)
	appendLog(filepath.Join(logDir, requestsLogFile), reqLogJSON)

	log.Printf("Request body size: %d bytes", len(bodyBytes))

	targetURL := fmt.Sprintf("%s/v1/messages?%s", targetBaseURL, r.URL.RawQuery)

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Failed to create proxy request", http.StatusBadGateway)
		return
	}

	for k, v := range r.Header {
		proxyReq.Header[k] = v
	}
	proxyReq.Header.Del("Transfer-Encoding")

	logEntry = fmt.Sprintf("=== CLAUDEFORGE RESPONSE [%s] ===", reqID)
	log.Println(logEntry)
	log.Printf("Forwarding to: %s", targetURL)

	resp, err := client.Transport.RoundTrip(proxyReq)
	if err != nil {
		log.Printf("Error forwarding request: %v", err)
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		respHeaders[k] = strings.Join(v, ", ")
	}

	contentType := resp.Header.Get("Content-Type")
	isStreaming := strings.Contains(contentType, "text/event-stream")

	log.Printf("Response status: %d", resp.StatusCode)
	log.Printf("Response Content-Type: %s", contentType)
	log.Printf("Response streaming: %v", isStreaming)

	if isStreaming {
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		handleStreamingResponse(w, resp, reqID, timestamp)
	} else {
		handleNonStreamingResponse(w, resp, reqID, timestamp)
	}
}

func handleStreamingResponse(w http.ResponseWriter, resp *http.Response, reqID, timestamp string) {
	var fullBody bytes.Buffer
	flusher, canFlush := w.(http.Flusher)

	reader := resp.Body
	isGzip := resp.Header.Get("Content-Encoding") == "gzip"
	if isGzip {
		gzReader, err := gzip.NewReader(reader)
		if err != nil {
			log.Printf("Error creating gzip reader: %v", err)
			return
		}
		reader = gzReader
		defer gzReader.Close()
	}

	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			fullBody.Write(buf[:n])
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
			log.Printf("[%s] SSE chunk: %s", reqID, string(buf[:n]))
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error reading SSE body: %v", err)
			break
		}
	}

	respLog := responseLog{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Status:    resp.StatusCode,
		Headers:   nil,
		Body:      fullBody.String(),
	}

	respLogJSON, _ := json.MarshalIndent(respLog, "", "  ")
	appendLog(filepath.Join(logDir, responsesLogFile), respLogJSON)
	saveLog(filepath.Join(logDir, fmt.Sprintf("response_%s.json", strings.ReplaceAll(timestamp, ":", "-"))), respLogJSON)

	logEntry := fmt.Sprintf("=== CLAUDEFORGE COMPLETE [%s] - %d bytes ===", reqID, fullBody.Len())
	log.Println(logEntry)
}

func handleNonStreamingResponse(w http.ResponseWriter, resp *http.Response, reqID, timestamp string) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
	}

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(bodyBytes)

	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		respHeaders[k] = strings.Join(v, ", ")
	}

	var bodyStr string
	if len(bodyBytes) > 0 {
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err == nil {
			bodyStr = prettyJSON.String()
		} else {
			bodyStr = string(bodyBytes)
		}
	}

	respLog := responseLog{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Status:    resp.StatusCode,
		Headers:   respHeaders,
		Body:      bodyStr,
	}

	respLogJSON, _ := json.MarshalIndent(respLog, "", "  ")
	log.Println(string(respLogJSON))

	appendLog(filepath.Join(logDir, responsesLogFile), respLogJSON)
	saveLog(filepath.Join(logDir, fmt.Sprintf("response_%s.json", strings.ReplaceAll(timestamp, ":", "-"))), respLogJSON)

	logEntry := fmt.Sprintf("=== CLAUDEFORGE COMPLETE [%s] ===", reqID)
	log.Println(logEntry)
}

func saveLog(filename string, data []byte) {
	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("Error saving log file %s: %v", filename, err)
	}
}

func appendLog(filename string, data []byte) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening log file %s: %v", filename, err)
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		log.Printf("Error appending to log file %s: %v", filename, err)
		return
	}
	if _, err := f.WriteString("\n"); err != nil {
		log.Printf("Error writing newline to log file %s: %v", filename, err)
	}
}

type decompressingTransport struct{}

func (t *decompressingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.Header.Get("Content-Encoding") == "gzip" {
		body, err := gzip.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		resp.Body = body
		resp.Header.Del("Content-Encoding")
	}

	return resp, nil
}

var _ http.RoundTripper = (*decompressingTransport)(nil)

func newClient() *http.Client {
	return &http.Client{
		Transport: &decompressingTransport{},
		Timeout:   5 * time.Minute,
	}
}
