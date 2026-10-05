// Command missioncontrol runs LaunchPad's REST + WebSocket front end. It
// speaks HTTP/1.1 and, via h2c, HTTP/2 over plaintext — so a VegaLoad (or
// any other) load test can exercise both without needing a TLS
// certificate just to try the demo. It talks to the engine service over
// gRPC to drive and receive live telemetry.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/umamukkara/launchpad/internal/api"
)

func main() {
	httpAddr := flag.String("addr", ":8080", "address for the HTTP(S)/WebSocket server to listen on")
	engineAddr := flag.String("engine", envOr("LAUNCHPAD_ENGINE_ADDR", "localhost:7070"), "address of the engine gRPC service")
	flag.Parse()

	srv, err := api.New(*engineAddr)
	if err != nil {
		log.Fatalf("missioncontrol: dial engine at %s: %v", *engineAddr, err)
	}
	defer srv.Close()

	mux := http.NewServeMux()
	srv.Routes(mux)
	mux.Handle("/", http.FileServer(http.Dir("web")))

	// h2c lets HTTP/2 run over plain TCP (no TLS), which keeps LaunchPad
	// trivial to run locally while still giving an HTTP/2 target for
	// load tests that want one. Point a load test at TLS + ALPN h2 in
	// front of this (e.g. a reverse proxy) if you need "real" HTTP/2.
	h2s := &http2.Server{}
	handler := h2c.NewHandler(mux, h2s)

	log.Printf("launchpad-missioncontrol listening on %s (HTTP/1.1 + h2c), engine at %s", *httpAddr, *engineAddr)
	if err := http.ListenAndServe(*httpAddr, handler); err != nil {
		log.Fatalf("missioncontrol: serve: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
