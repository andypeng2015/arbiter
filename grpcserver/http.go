package grpcserver

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewHTTPServer creates an HTTP server that exposes Prometheus metrics and
// operational health endpoints on the given address. The server is separate
// from the gRPC listener so that scraping and health checks do not share
// the gRPC port.
//
// Endpoints:
//
//	GET /metrics  — Prometheus text exposition format
//	GET /healthz  — liveness probe (always 200 ok)
//	GET /readyz   — readiness probe (always 200 ok once server is started)
//	GET /status   — JSON summary of server identity
func NewHTTPServer(addr string, reg *prometheus.Registry) *http.Server {
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service": "arbiter-grpc",
			"status":  "running",
		})
	})

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}
