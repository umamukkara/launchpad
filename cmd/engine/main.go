// Command engine runs the LaunchPad engine gRPC service standalone.
// It is also mounted in-process by mission-control when LaunchPad runs
// in monolith mode (see cmd/missioncontrol).
package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/umamukkara/launchpad/internal/metrics"
	"github.com/umamukkara/launchpad/internal/telemetry"
	pb "github.com/umamukkara/launchpad/internal/telemetry/telemetrypb"
)

func main() {
	addr := flag.String("addr", ":7070", "address for the gRPC server to listen on")
	metricsAddr := flag.String("metrics-addr", ":9100", "address to serve /metrics on (gRPC can't share a port with plain HTTP)")
	flag.Parse()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("engine: listen %s: %v", *addr, err)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(metrics.UnaryServerInterceptor),
		grpc.ChainStreamInterceptor(metrics.StreamServerInterceptor),
	)
	pb.RegisterEngineControlServer(grpcServer, telemetry.NewServer())
	reflection.Register(grpcServer) // handy for grpcurl / ad hoc probing

	go func() {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", metrics.Handler())
		log.Printf("launchpad-engine metrics on %s/metrics", *metricsAddr)
		if err := http.ListenAndServe(*metricsAddr, mux); err != nil {
			log.Fatalf("engine: metrics server: %v", err)
		}
	}()

	log.Printf("launchpad-engine listening on %s (gRPC)", *addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("engine: serve: %v", err)
	}
}
