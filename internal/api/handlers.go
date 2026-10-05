package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/umamukkara/launchpad/internal/metrics"
	pb "github.com/umamukkara/launchpad/internal/telemetry/telemetrypb"
	"github.com/umamukkara/launchpad/internal/ws"
)

// Server wires the REST API, the per-launch WebSocket hubs, and the gRPC
// client to the engine service together. It is the main HTTP handler
// mission-control registers routes against.
type Server struct {
	store      *Store
	engineAddr string
	engineConn *grpc.ClientConn
	engine     pb.EngineControlClient

	hubsMu sync.Mutex
	hubs   map[string]*ws.Hub // launchID -> hub
}

// New dials the engine service and returns a ready-to-use Server.
// Dialing uses grpc.NewClient (non-blocking); the first RPC will connect.
func New(engineAddr string) (*Server, error) {
	conn, err := grpc.NewClient(engineAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Server{
		store:      NewStore(),
		engineAddr: engineAddr,
		engineConn: conn,
		engine:     pb.NewEngineControlClient(conn),
		hubs:       make(map[string]*ws.Hub),
	}, nil
}

func (s *Server) hubFor(launchID string) *ws.Hub {
	s.hubsMu.Lock()
	defer s.hubsMu.Unlock()
	h, ok := s.hubs[launchID]
	if !ok {
		h = ws.NewHub()
		s.hubs[launchID] = h
	}
	return h
}

// Routes registers every HTTP route LaunchPad exposes onto mux. Each route
// (other than /metrics itself) is wrapped with Prometheus instrumentation
// so launchpad_http_requests_total and launchpad_http_request_duration_seconds
// cover every request regardless of what sent it — the dashboard, curl, or
// a VegaLoad scenario.
func (s *Server) Routes(mux *http.ServeMux) {
	route := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, instrument(pattern, h))
	}

	route("GET /api/crew", s.listCrew)
	route("POST /api/crew", s.addCrew)
	route("GET /api/crew/{id}", s.getCrew)
	route("DELETE /api/crew/{id}", s.deleteCrew)

	route("GET /api/launches", s.listLaunches)
	route("POST /api/launches", s.scheduleLaunch)
	route("GET /api/launches/{id}", s.getLaunch)
	route("POST /api/launches/{id}/ignite", s.ignite)
	route("POST /api/launches/{id}/abort", s.abort)

	route("GET /api/engine/status", s.engineStatus)
	route("GET /healthz", s.healthz)

	route("GET /ws/launches/{id}", s.launchWS)

	// Not wrapped with instrument(): scraping /metrics shouldn't count
	// towards the very metrics it's reading.
	mux.Handle("GET /metrics", metrics.Handler())
}

// instrument wraps h so every call to it records a request count and a
// duration observation, labeled by the route pattern given at
// registration time (never the raw path, which would blow up cardinality
// with one series per launch/crew ID).
func instrument(pattern string, h http.HandlerFunc) http.HandlerFunc {
	method, route := splitPattern(pattern)
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h(sw, r)
		metrics.HTTPRequestDuration.WithLabelValues(route, method).Observe(time.Since(start).Seconds())
		metrics.HTTPRequestsTotal.WithLabelValues(route, method, strconv.Itoa(sw.status)).Inc()
	}
}

// splitPattern splits a Go 1.22+ ServeMux pattern ("GET /api/crew/{id}")
// into its method and path parts.
func splitPattern(pattern string) (method, route string) {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == ' ' {
			return pattern[:i], pattern[i+1:]
		}
	}
	return "", pattern
}

// statusWriter captures the status code a handler wrote, since
// http.ResponseWriter doesn't expose it after the fact.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- crew ---

func (s *Server) listCrew(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListCrew())
}

func (s *Server) addCrew(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name, Role string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	m := s.store.AddCrew(body.Name, body.Role)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) getCrew(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, ok := s.store.GetCrew(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) deleteCrew(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.store.DeleteCrew(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- launches ---

func (s *Server) listLaunches(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListLaunches())
}

func (s *Server) scheduleLaunch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Vehicle        string   `json:"vehicle"`
		CrewIDs        []string `json:"crew_ids"`
		TargetThrustKN float64  `json:"target_thrust_kn"`
		DurationSec    int32    `json:"duration_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Vehicle == "" {
		body.Vehicle = "vega-c"
	}
	l := s.store.ScheduleLaunch(body.Vehicle, body.CrewIDs, body.TargetThrustKN, body.DurationSec)
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) getLaunch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	l, ok := s.store.GetLaunch(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// ignite triggers the gRPC Ignite stream against the engine service and
// relays every telemetry frame onto this launch's WebSocket hub as it
// arrives. The HTTP response returns immediately (202 Accepted); the
// actual telemetry is only visible over /ws/launches/{id}.
func (s *Server) ignite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	l, ok := s.store.GetLaunch(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	stream, err := s.engine.Ignite(context.Background(), &pb.IgniteRequest{
		LaunchId:        l.ID,
		Vehicle:         l.Vehicle,
		TargetThrustKn:  l.TargetThrustKN,
		DurationSeconds: l.DurationSec,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.store.SetLaunchStatus(id, StatusInFlight)
	hub := s.hubFor(id)

	go func() {
		for {
			frame, err := stream.Recv()
			if err != nil {
				return
			}
			hub.Broadcast(frame)
			if frame.GetStage() == pb.Stage_STAGE_COMPLETE {
				s.store.SetLaunchStatus(id, StatusComplete)
			} else if frame.GetStage() == pb.Stage_STAGE_ABORTED {
				s.store.SetLaunchStatus(id, StatusAborted)
			}
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "ignition started",
		"ws_url": "/ws/launches/" + id,
	})
}

func (s *Server) abort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct{ Reason string }
	_ = json.NewDecoder(r.Body).Decode(&body)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.engine.Abort(ctx, &pb.AbortRequest{LaunchId: id, Reason: body.Reason})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) engineStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	resp, err := s.engine.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) launchWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetLaunch(id); !ok {
		http.Error(w, "launch not found", http.StatusNotFound)
		return
	}
	s.hubFor(id).ServeHTTP(w, r)
}

// Close releases the gRPC connection to the engine service.
func (s *Server) Close() {
	if err := s.engineConn.Close(); err != nil {
		log.Printf("closing engine conn: %v", err)
	}
}
