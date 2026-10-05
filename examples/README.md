# Example load-test scenarios

This folder is where VegaLoad scenario scripts (JavaScript/TypeScript or
Python) against LaunchPad belong once VegaLoad's scripting API is wired
up end to end.

For now, LaunchPad's three targets are:

| Protocol  | Target                                         | Notes |
|-----------|-------------------------------------------------|-------|
| HTTP/1.1  | `http://localhost:8080/api/...`                 | REST: crew + launch CRUD |
| HTTP/2    | `http://localhost:8080/...` (h2c, no TLS needed)| Same routes, negotiated via h2c |
| gRPC      | `localhost:7070`, service `launchpad.telemetry.v1.EngineControl` | `Ignite` (server-streaming), `Abort` and `Status` (unary) |
| WebSocket | `ws://localhost:8080/ws/launches/{id}`          | Live telemetry fan-out, read-only from the client's side |

A typical scenario flow: `POST /api/launches` to schedule a launch, open
the WebSocket for that launch ID, then `POST /api/launches/{id}/ignite`
and read frames off the socket until `STAGE_COMPLETE`. See the repo
README for the full request/response shapes.
