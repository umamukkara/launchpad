// Package telemetry implements the EngineControl gRPC service: a small,
// deliberately simple simulation of a rocket engine burn. It exists to give
// VegaLoad (and anyone evaluating it) a realistic gRPC target: one
// server-streaming RPC (Ignite), one short unary call (Abort), and one
// cheap, high-QPS unary call (Status).
package telemetry

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/umamukkara/launchpad/internal/metrics"
	pb "github.com/umamukkara/launchpad/internal/telemetry/telemetrypb"
)

// Server implements pb.EngineControlServer.
type Server struct {
	pb.UnimplementedEngineControlServer

	mu     sync.Mutex
	active map[string]chan struct{} // launchID -> abort signal
}

// NewServer constructs an engine Server ready to serve.
func NewServer() *Server {
	return &Server{active: make(map[string]chan struct{})}
}

// Ignite simulates an engine burn and streams one TelemetryFrame roughly
// every 100ms of simulated flight time until the burn completes, is
// aborted, or the client disconnects.
func (s *Server) Ignite(req *pb.IgniteRequest, stream pb.EngineControl_IgniteServer) error {
	if req.GetLaunchId() == "" {
		return fmt.Errorf("launch_id is required")
	}
	duration := req.GetDurationSeconds()
	if duration <= 0 {
		duration = 30
	}
	targetThrust := req.GetTargetThrustKn()
	if targetThrust <= 0 {
		targetThrust = 2150 // Vega-class first stage, kN, illustrative only
	}

	abort := make(chan struct{})
	s.mu.Lock()
	s.active[req.LaunchId] = abort
	s.mu.Unlock()
	metrics.EngineActiveBurns.Inc()
	defer func() {
		s.mu.Lock()
		delete(s.active, req.LaunchId)
		s.mu.Unlock()
		metrics.EngineActiveBurns.Dec()
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	totalTicks := int(duration) * 10
	var fuel float64 = 100

	for tick := 0; tick <= totalTicks; tick++ {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-abort:
			frame := &pb.TelemetryFrame{
				LaunchId: req.LaunchId,
				TPlusMs:  int64(tick) * 100,
				Stage:    pb.Stage_STAGE_ABORTED,
				Note:     "engine shutdown on abort command",
			}
			return stream.Send(frame)
		case <-ticker.C:
		}

		progress := float64(tick) / float64(totalTicks)
		fuel = math.Max(0, 100-progress*95)

		frame := &pb.TelemetryFrame{
			LaunchId:    req.LaunchId,
			TPlusMs:     int64(tick) * 100,
			AltitudeM:   progress * progress * 120000,
			VelocityMps: progress * 2400,
			ThrustKn:    targetThrust * (0.6 + 0.4*math.Min(1, progress*4)),
			FuelPct:     fuel,
			Stage:       stageFor(progress),
			Note:        noteFor(progress),
		}

		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	return nil
}

func stageFor(progress float64) pb.Stage {
	switch {
	case progress < 0.02:
		return pb.Stage_STAGE_IGNITION
	case progress < 0.55:
		return pb.Stage_STAGE_ASCENT
	case progress < 0.6:
		return pb.Stage_STAGE_SEPARATION
	case progress < 0.98:
		return pb.Stage_STAGE_ORBIT_INSERTION
	default:
		return pb.Stage_STAGE_COMPLETE
	}
}

func noteFor(progress float64) string {
	switch {
	case progress < 0.02:
		return "ignition"
	case progress >= 0.55 && progress < 0.6:
		return "stage separation"
	case progress >= 0.98:
		return "MECO"
	default:
		return ""
	}
}

// Abort signals a running Ignite stream for launchID to stop early.
func (s *Server) Abort(ctx context.Context, req *pb.AbortRequest) (*pb.AbortResponse, error) {
	s.mu.Lock()
	ch, ok := s.active[req.LaunchId]
	s.mu.Unlock()
	if !ok {
		return &pb.AbortResponse{Accepted: false, Message: "no active burn for launch_id"}, nil
	}
	select {
	case ch <- struct{}{}:
	default:
	}
	log.Printf("abort requested for launch %s: %s", req.LaunchId, req.Reason)
	return &pb.AbortResponse{Accepted: true, Message: "abort signal sent"}, nil
}

// Status is a cheap unary call: a good target for pure-QPS gRPC load tests
// that aren't trying to exercise streaming at all.
func (s *Server) Status(ctx context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	s.mu.Lock()
	n := len(s.active)
	s.mu.Unlock()
	return &pb.StatusResponse{ActiveBurns: int32(n), EngineVersion: "launchpad-engine/0.1"}, nil
}
