package otlp

import (
	"compress/gzip"
	"context"
	"io"
	"log"
	"net/http"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
)

// Server is the local OTLP http/protobuf receiver, implementing the logs,
// metrics, and traces endpoints.
type Server struct {
	writer        *ingest.Writer
	metricsWriter *ingest.MetricsWriter
	spansWriter   *ingest.SpansWriter
	http          *http.Server
}

// NewServer builds a Server bound to addr (must be a 127.0.0.1 address —
// this receiver is never meant to see traffic beyond localhost).
func NewServer(addr string, w *ingest.Writer, mw *ingest.MetricsWriter, sw *ingest.SpansWriter) *Server {
	mux := http.NewServeMux()
	s := &Server{writer: w, metricsWriter: mw, spansWriter: sw}
	mux.HandleFunc("/v1/logs", s.handleLogs)
	mux.HandleFunc("/v1/metrics", s.handleMetrics)
	mux.HandleFunc("/v1/traces", s.handleTraces)
	s.http = &http.Server{Addr: addr, Handler: mux}
	return s
}

// ListenAndServe blocks serving until the server is shut down, mirroring
// http.Server.ListenAndServe (returns http.ErrServerClosed on clean
// shutdown).
func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	events, err := decodeLogsRequest(body)
	if err != nil {
		log.Printf("otlp: failed to decode /v1/logs payload: %v", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	log.Printf("otlp: /v1/logs received %d bytes, decoded %d events", len(body), len(events))
	for _, e := range events {
		s.writer.Enqueue(e)
	}

	respondEmptyOTLPResponse(w)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	points, err := decodeMetricsRequest(body)
	if err != nil {
		log.Printf("otlp: failed to decode /v1/metrics payload: %v", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	log.Printf("otlp: /v1/metrics received %d bytes, decoded %d points", len(body), len(points))
	for _, p := range points {
		s.metricsWriter.Enqueue(p)
	}

	respondEmptyOTLPResponse(w)
}

func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	spans, err := decodeTracesRequest(body)
	if err != nil {
		log.Printf("otlp: failed to decode /v1/traces payload: %v", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	log.Printf("otlp: /v1/traces received %d bytes, decoded %d spans", len(body), len(spans))
	for _, sp := range spans {
		s.spansWriter.Enqueue(sp)
	}

	respondEmptyOTLPResponse(w)
}

// readBody returns the request body, transparently gunzipping it if
// Content-Encoding: gzip is set (OTLP exporters may compress payloads).
func readBody(r *http.Request) ([]byte, error) {
	reader := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	return io.ReadAll(reader)
}

// respondEmptyOTLPResponse writes a valid, fully-accepted OTLP export
// response (Export{Logs,Metrics,Traces}ServiceResponse). Each one's only
// field (partial_success) is optional; per proto3 wire format a message
// with every field unset serializes to zero bytes, so an empty body is a
// correctly-encoded "no rejections" response for all three — no need to
// depend on the generated response types for this. We respond as fast as
// possible: the export is already handed off to the buffered writer by the
// time this is called.
func respondEmptyOTLPResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}
