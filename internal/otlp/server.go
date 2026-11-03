package otlp

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/localhttp"
	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
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
	// Only the agents on this machine may send here. A web page could otherwise
	// plant telemetry, and so text in suggested fixes, or exhaust memory.
	s.http = localhttp.Server(addr, logging.RecoverHTTP("otlp", localhttp.Guard(protobufOnly(mux))))
	s.http.ErrorLog = slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn)
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

	body, err := readBody(w, r)
	if err != nil {
		bodyError(w, err)
		return
	}

	events, err := decodeLogsRequest(body)
	if err != nil {
		slog.Warn("otlp: failed to decode payload", "path", "/v1/logs", "err", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	slog.Debug("otlp: received", "path", "/v1/logs", "bytes", len(body), "events", len(events))
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

	body, err := readBody(w, r)
	if err != nil {
		bodyError(w, err)
		return
	}

	points, err := decodeMetricsRequest(body)
	if err != nil {
		slog.Warn("otlp: failed to decode payload", "path", "/v1/metrics", "err", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	slog.Debug("otlp: received", "path", "/v1/metrics", "bytes", len(body), "points", len(points))
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

	body, err := readBody(w, r)
	if err != nil {
		bodyError(w, err)
		return
	}

	spans, err := decodeTracesRequest(body)
	if err != nil {
		slog.Warn("otlp: failed to decode payload", "path", "/v1/traces", "err", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	slog.Debug("otlp: received", "path", "/v1/traces", "bytes", len(body), "spans", len(spans))
	for _, sp := range spans {
		s.spansWriter.Enqueue(sp)
	}

	respondEmptyOTLPResponse(w)
}

// Limits on one export. Real exports are far smaller, and gzip can turn a
// small request into a huge one, so the decompressed size is capped too.
const (
	maxBodyBytes         = 16 << 20
	maxDecompressedBytes = 64 << 20
)

// errTooLarge is returned for a payload over the limits.
var errTooLarge = errors.New("payload too large")

// protobufOnly refuses anything but OTLP's binary encoding. Every exporter
// sends it, and a web page can't: the browser would ask permission first,
// which this server never gives.
func protobufOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Method == http.MethodPost && (err != nil || (mediaType != "application/x-protobuf" && mediaType != "application/protobuf")) {
			// Logged, so an agent sending JSON shows up in `token-advisor logs`.
			slog.Warn("otlp: refused a request that isn't protobuf", "path", r.URL.Path, "content_type", r.Header.Get("Content-Type"))
			http.Error(w, "only application/x-protobuf is accepted", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// readBody returns the request body, transparently gunzipping it if
// Content-Encoding: gzip is set (OTLP exporters may compress payloads).
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	reader := io.Reader(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxDecompressedBytes+1))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) || len(body) > maxDecompressedBytes {
		return nil, errTooLarge
	}
	return body, err
}

// bodyError answers a body that couldn't be read.
func bodyError(w http.ResponseWriter, err error) {
	if errors.Is(err, errTooLarge) {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "failed to read body", http.StatusBadRequest)
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
