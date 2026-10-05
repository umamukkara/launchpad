// Command engine runs the LaunchPad engine gRPC service standalone.
// It is also mounted in-process by mission-control when LaunchPad runs
// in monolith mode (see cmd/missioncontrol).
package main

import (
	"flag"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/umamukkara/launchpad/internal/telemetry"
	pb "github.com/umamukkara/launchpad/internal/telemetry/telemetrypb"
)

func main() {
	addr := flag.String("addr", ":7070", "address for the gRPC server to listen on")
	flag.Parse()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("engine: listen %s: %v", *addr, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterEngineControlServer(grpcServer, telemetry.NewServer())
	reflection.Register(grpcServer) // handy for grpcurl / ad hoc probing

	log.Printf("launchpad-engine listening on %s (gRPC)", *addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("engine: serve: %v", err)
	}
}
