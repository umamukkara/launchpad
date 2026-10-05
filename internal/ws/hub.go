// Package ws implements the live-telemetry WebSocket fan-out: one hub per
// running launch, broadcasting the same frames mission-control receives
// over gRPC from the engine service out to every connected dashboard (or
// load-test) client. This is LaunchPad's WebSocket surface.
package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/umamukkara/launchpad/internal/metrics"
)

var upgrader = websocket.Upgrader{
	// LaunchPad is a load-testing fixture meant to be hit from anywhere
	// (including a VegaLoad scenario running on a different host), so it
	// deliberately does not enforce an Origin check. Do not copy this
	// into anything that isn't a throwaway test target.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub fans one launch's telemetry out to any number of subscribers.
type Hub struct {
	mu   sync.RWMutex
	subs map[*websocket.Conn]chan []byte
}

func NewHub() *Hub {
	return &Hub{subs: make(map[*websocket.Conn]chan []byte)}
}

// ServeHTTP upgrades the request to a WebSocket and keeps the connection
// registered until the client disconnects. Each connection gets its own
// small buffered channel so one slow reader can't block broadcasts to
// everyone else.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	ch := make(chan []byte, 32)

	h.mu.Lock()
	h.subs[conn] = ch
	h.mu.Unlock()
	metrics.WebSocketConnectionsActive.Inc()

	defer func() {
		h.mu.Lock()
		delete(h.subs, conn)
		h.mu.Unlock()
		metrics.WebSocketConnectionsActive.Dec()
		conn.Close()
	}()

	// Drain client->server messages (LaunchPad doesn't expect any, but a
	// client is allowed to send e.g. ping/pong control frames) in one
	// goroutine, write broadcast frames in this one.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				conn.Close()
				return
			}
		}
	}()

	for msg := range ch {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

// Broadcast marshals v as JSON and sends it to every subscriber currently
// connected to this hub. Slow or dead subscribers are skipped, not blocked
// on.
func (h *Hub) Broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("ws broadcast marshal: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- b:
		default:
			// subscriber is backed up; drop the frame rather than block.
		}
	}
}

// Subscribers reports the current connection count, mainly for the
// /api/engine/status style introspection endpoints.
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
