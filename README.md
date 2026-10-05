# LaunchPad

LaunchPad is a small, deliberately simple sample application built to be
load-tested. It exists mainly as a free, self-contained target for trying
out [VegaLoad](https://github.com/vegaload/vegaload), but it has no
VegaLoad-specific dependency — point any load-testing tool at it.

It's a mission-control simulator: schedule a launch, ignite it, and watch
simulated engine telemetry stream in live. Underneath, that one user
story touches all four protocols a general-purpose load tester usually
needs to cover:

| Protocol  | Where                                            |
|-----------|---------------------------------------------------|
| HTTP/1.1  | REST API — crew and launch CRUD                   |
| HTTP/2    | Same REST API, over h2c (no TLS needed locally)    |
| gRPC      | `engine` service — one streaming RPC, two unary    |
| WebSocket | Live telemetry fan-out per launch                  |

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how the two
services fit together, and [`examples/`](examples/README.md) for the
endpoints to point a load test at.

## Run it

Requires Go 1.26+.

```sh
# terminal 1
go run ./cmd/engine

# terminal 2
go run ./cmd/missioncontrol
```

Then open http://localhost:8080 for the dashboard. Schedule a launch, click
ignite, and watch live engine telemetry stream in — both as raw JSON frames
and as a line chart (time since ignition on the x-axis, your choice of
thrust/altitude/velocity/fuel on the y-axis, with a hover crosshair for
exact values). It's a quick visual way to see a metric ramp up in real
time, which is the same shape you'd want from a load test's own live
results.

Or drive it directly from the terminal:

```sh
# schedule a launch
curl -s -X POST localhost:8080/api/launches \
  -d '{"vehicle":"vega-c","target_thrust_kn":2150,"duration_seconds":20}'
# => {"id":"launch-0001", ...}

# in another terminal: watch telemetry
# (any WebSocket client works; wscat/websocat/etc.)
websocat ws://localhost:8080/ws/launches/launch-0001

# back in the first terminal: start the burn
curl -s -X POST localhost:8080/api/launches/launch-0001/ignite
```

### With Docker

```sh
docker compose -f deploy/docker-compose.yml up --build
```

## API surface

**REST** (`missioncontrol`, port 8080)

- `GET /api/crew`, `POST /api/crew`, `GET /api/crew/{id}`, `DELETE /api/crew/{id}`
- `GET /api/launches`, `POST /api/launches`, `GET /api/launches/{id}`
- `POST /api/launches/{id}/ignite` — starts the gRPC `Ignite` stream, returns immediately
- `POST /api/launches/{id}/abort` — calls gRPC `Abort`
- `GET /api/engine/status` — calls gRPC `Status`
- `GET /healthz`

**WebSocket**

- `GET /ws/launches/{id}` — subscribe to live `TelemetryFrame`s (JSON) for that launch

**gRPC** (`engine`, port 7070, service `launchpad.telemetry.v1.EngineControl`, see `proto/telemetry.proto`)

- `Ignite(IgniteRequest) returns (stream TelemetryFrame)`
- `Abort(AbortRequest) returns (AbortResponse)`
- `Status(StatusRequest) returns (StatusResponse)`

Server reflection is enabled, so `grpcurl -plaintext localhost:7070 list`
works out of the box.

## Regenerating the gRPC code

If you edit `proto/telemetry.proto`, regenerate with:

```sh
protoc --go_out=. --go_opt=module=github.com/umamukkara/launchpad \
       --go-grpc_out=. --go-grpc_opt=module=github.com/umamukkara/launchpad \
       proto/telemetry.proto
```

(Requires `protoc`, plus `protoc-gen-go` and `protoc-gen-go-grpc` on your `PATH`.)

## Status

Early and intentionally minimal: no persistence, no auth, no TLS — see
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md#whats-deliberately-not-here)
for why. Contributions and issues welcome.

## License

Apache-2.0 — see [LICENSE](LICENSE).
