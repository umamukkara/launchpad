# Resilience examples (chaos + load)

LaunchPad is a local chaos target as well as a load target.

- `GET /healthz` — process is up (does not call engine)
- `GET /api/launches` — in-memory store (does not call engine)
- `GET /api/engine/status` — unary gRPC to engine, **3s timeout**, HTTP 502 if engine is down or SIGSTOP'd
- `POST /api/launches/{id}/ignite` — waits for engine (same timeout) then starts the stream; `?retries=N` or `LAUNCHPAD_IGNITE_RETRIES`

Point HTTP-proxy faults at a listen address in front of `:8080`. Point **process.pause** probes at `/api/engine/status`, not `/api/launches`.

Checked-in companion files live in [litmus-lite](https://github.com/ksatchit/litmus-lite) `examples/launchpad/` (HTTP latency, pause-engine, VegaLoad). Copy or import those beside `cmd/missioncontrol` rather than forking them here.
