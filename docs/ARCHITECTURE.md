# LaunchPad architecture

LaunchPad is two small Go services plus a static dashboard page. It exists
to give load-testing tools (VegaLoad in particular) a realistic, free
target that exercises HTTP/1.1, HTTP/2 (h2c), gRPC, and WebSocket in one
place, without modeling anything that matters.

```
                     schedules / lists
        ┌───────────────────────────────────┐
        │                                    │
        ▼                                    │
┌───────────────┐   gRPC: Ignite/Abort/Status  ┌──────────────┐
│ missioncontrol │ ──────────────────────────► │    engine    │
│  (REST + WS)   │ ◄────── telemetry stream ── │  (gRPC only) │
└───────┬────────┘                             └──────────────┘
        │  broadcasts frames
        ▼
┌────────────────┐
│  WebSocket      │   one hub per launch; any number of
│  subscribers    │   dashboard / load-test clients can
│  (dashboard,    │   connect and receive the same frames
│  load tests)    │
└────────────────┘
```

## Services

**`cmd/engine`** is a standalone gRPC server (`internal/telemetry`). It
simulates a rocket engine burn:

- `Ignite` — server-streaming RPC. Accepts a launch ID, vehicle, target
  thrust, and duration; streams one `TelemetryFrame` roughly every 100ms
  of simulated flight time until the burn completes or is aborted.
- `Abort` — unary RPC. Signals a running `Ignite` stream to stop early.
- `Status` — unary RPC. Cheap, high-QPS health/state check, useful as a
  gRPC load target that deliberately isn't a stream.

**`cmd/missioncontrol`** is the REST + WebSocket front end
(`internal/api`, `internal/ws`). It holds an in-memory store of crew
members and launches (no database — this is a load-testing fixture, not
a durability demo), calls the engine service over gRPC when a launch is
ignited or aborted, and fans every telemetry frame it receives out to
whichever WebSocket clients are subscribed to that launch's channel.

Mission-control's HTTP server is wrapped with `golang.org/x/net/http2/h2c`,
so it speaks both HTTP/1.1 and HTTP/2 without needing a TLS certificate —
useful for a sample app people will run locally, less useful as a model
for anything that actually needs transport security.

## Why split into two services at all

A single binary would be simpler, but LaunchPad is meant to be a
believable *target*, not just a protocol-feature checklist. Splitting the
gRPC surface into its own service means a load test pointed at gRPC is
actually hitting a different process over the network, not calling a
function in the same binary — closer to what load-testing a real
microservice looks like. `deploy/docker-compose.yml` runs them as two
containers for exactly this reason.

## Observability

Both services are instrumented with Prometheus (`internal/metrics`):

- `missioncontrol` wraps every REST route in middleware that records a
  request count and duration, labeled by *route pattern* (e.g.
  `/api/crew/{id}`) rather than the raw path, so one crew ID doesn't
  become its own metrics series. It also tracks a gauge of currently open
  WebSocket subscriptions. All of this is served at `GET /metrics` on its
  normal port (8080).
- `engine` wraps every gRPC call (unary and streaming) in an interceptor
  that records the same kind of count/duration, labeled by method and
  gRPC status code, plus a gauge of currently running `Ignite` streams.
  Since its main port only speaks gRPC, `/metrics` is served on a second,
  plain-HTTP port (9100 by default).

This is deliberately decoupled from who's generating the traffic. The
dashboard's own telemetry chart only shows a launch you ignited from that
browser tab; these metrics cover *every* request, from any source —
curl, the dashboard, or a VegaLoad scenario — which is what you actually
want when watching a load test run. `deploy/docker-compose.yml` adds
Prometheus (scraping both `/metrics` endpoints every 5s) and Grafana
(pre-provisioned with a dashboard reading from that Prometheus) as two
more containers, so `docker compose up` gets you a live view at
http://localhost:3000 with no extra setup. See the README's
"Watching live traffic" section for the panel list.

## What's deliberately not here

- No persistence. Restarting either process resets all state.
- No auth. Every endpoint is open, on purpose — this is a test fixture.
- No TLS. `h2c` and plaintext gRPC keep local setup to "go run" or
  "docker compose up," at the cost of not being a template for anything
  production-facing.
