package metrics

import (
	"context"
	"path"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor records launchpad_grpc_requests_total and
// launchpad_grpc_request_duration_seconds for every unary RPC (Abort,
// Status). Ignite is a server-streaming RPC and goes through
// StreamServerInterceptor instead.
func UnaryServerInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	observe(info.FullMethod, start, err)
	return resp, err
}

// StreamServerInterceptor records the same metrics for streaming RPCs
// (Ignite). The duration observed is the full stream lifetime: from the
// first call to Ignite until the stream ends, not per-frame.
func StreamServerInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, ss)
	observe(info.FullMethod, start, err)
	return err
}

func observe(fullMethod string, start time.Time, err error) {
	method := path.Base(fullMethod) // "/launchpad.telemetry.v1.EngineControl/Ignite" -> "Ignite"
	GRPCRequestDuration.WithLabelValues(method).Observe(time.Since(start).Seconds())
	GRPCRequestsTotal.WithLabelValues(method, status.Code(err).String()).Inc()
}
