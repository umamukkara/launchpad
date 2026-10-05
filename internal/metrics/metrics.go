// Package metrics defines LaunchPad's Prometheus instrumentation: a small,
// fixed set of counters, histograms, and gauges covering the REST API, the
// gRPC engine service, and the WebSocket telemetry fan-out. This is what
// lets a dashboard (Grafana, or anything else that can query Prometheus)
// show "is this app under load right now," regardless of what sent the
// traffic — a VegaLoad scenario, curl, or the built-in dashboard page.
//
// Every metric here is registered once, in this package's init, onto the
// default Prometheus registry. Both cmd/missioncontrol and cmd/engine are
// separate processes, so there's no risk of double-registration across
// them — each binary gets its own registry instance at runtime.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTPRequestsTotal counts every REST request mission-control serves,
	// labeled by route pattern (not the raw path, so "/api/crew/{id}"
	// stays one series regardless of which crew ID was requested),
	// method, and status code.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "launchpad_http_requests_total",
		Help: "Total REST requests handled by mission-control.",
	}, []string{"route", "method", "status"})

	// HTTPRequestDuration measures how long each REST request took.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "launchpad_http_request_duration_seconds",
		Help:    "REST request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	// GRPCRequestsTotal counts every gRPC call the engine service
	// handles, labeled by method and the gRPC status code it returned.
	// For Ignite (a stream), one call is counted when the stream ends.
	GRPCRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "launchpad_grpc_requests_total",
		Help: "Total gRPC calls handled by the engine service.",
	}, []string{"method", "code"})

	// GRPCRequestDuration measures how long each gRPC call took. For
	// Ignite, that's the full stream lifetime, not a single frame.
	GRPCRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "launchpad_grpc_request_duration_seconds",
		Help:    "gRPC call duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	// WebSocketConnectionsActive is the number of dashboard/load-test
	// clients currently subscribed to any launch's telemetry feed.
	WebSocketConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "launchpad_websocket_connections_active",
		Help: "Number of currently open WebSocket telemetry subscriptions.",
	})

	// EngineActiveBurns is the number of Ignite streams currently
	// running inside the engine service.
	EngineActiveBurns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "launchpad_engine_active_burns",
		Help: "Number of engine burns (Ignite streams) currently in progress.",
	})
)

// Handler returns the HTTP handler that serves /metrics in Prometheus's
// text exposition format. Mount it on whichever HTTP server a service
// already runs (mission-control), or on a small dedicated listener for a
// service that otherwise speaks a different protocol (the engine's gRPC
// port).
func Handler() http.Handler {
	return promhttp.Handler()
}
