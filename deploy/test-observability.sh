#!/usr/bin/env bash
# test-observability.sh
#
# One-shot test of LaunchPad's Prometheus + Grafana setup.
# Run this from the repo root: ./deploy/test-observability.sh
#
# What it does, in order:
#   1. Builds and starts all four containers (engine, missioncontrol,
#      prometheus, grafana).
#   2. Waits for each one to respond.
#   3. Checks Prometheus sees both scrape targets as "up".
#   4. Sends some REST and gRPC traffic, so the Grafana graphs have
#      something to show.
#   5. Prints the URLs to open, and leaves everything running.
#
# Stop everything afterwards with:
#   docker compose -f deploy/docker-compose.yml down

set -euo pipefail

COMPOSE_FILE="deploy/docker-compose.yml"

# Run from the repo root, whatever directory this script was called from.
cd "$(dirname "$0")/.."

echo "==> Building and starting containers..."
docker compose -f "$COMPOSE_FILE" up --build -d

# wait_for <name> <url> waits until a URL responds, or gives up after 60s.
wait_for() {
  local name="$1" url="$2" tries=30
  echo -n "==> Waiting for $name ($url) "
  until curl -s -o /dev/null "$url"; do
    tries=$((tries - 1))
    if [ "$tries" -le 0 ]; then
      echo "FAILED"
      echo "    $name never responded at $url."
      echo "    Check its logs with: docker compose -f $COMPOSE_FILE logs"
      exit 1
    fi
    echo -n "."
    sleep 2
  done
  echo " up"
}

wait_for "mission-control"   "http://localhost:8080/healthz"
wait_for "engine metrics"    "http://localhost:9100/metrics"
wait_for "prometheus"        "http://localhost:9090/-/ready"
wait_for "grafana"           "http://localhost:3000/api/health"

echo "==> Checking Prometheus scrape targets..."
TARGETS_JSON=$(curl -s "http://localhost:9090/api/v1/targets")
UP_COUNT=$(echo "$TARGETS_JSON" | grep -o '"health":"up"' | wc -l | tr -d ' ')
echo "    $UP_COUNT target(s) reporting healthy (expect 2: missioncontrol, engine)"
if [ "$UP_COUNT" -lt 2 ]; then
  echo "    Not all targets are up yet. Open http://localhost:9090/targets to check why."
fi

echo "==> Sending test traffic..."

echo "    - 50 REST calls to /api/engine/status"
for _ in $(seq 1 50); do
  curl -s -o /dev/null "http://localhost:8080/api/engine/status"
done

echo "    - scheduling and igniting a launch (exercises REST + gRPC + WebSocket)"
LAUNCH_JSON=$(curl -s -X POST "http://localhost:8080/api/launches" \
  -d '{"vehicle":"vega-c","target_thrust_kn":2150,"duration_seconds":10}')
LAUNCH_ID=$(echo "$LAUNCH_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "      launch id: $LAUNCH_ID"
curl -s -X POST "http://localhost:8080/api/launches/$LAUNCH_ID/ignite" > /dev/null
echo "      ignited — burn runs for ~10s in the background"

echo ""
echo "==> Done. Everything is running. Open these in your browser:"
echo ""
echo "    Grafana dashboard:   http://localhost:3000"
echo "                         (look for 'LaunchPad — live traffic')"
echo "    Prometheus targets:  http://localhost:9090/targets"
echo "    Raw mission-control metrics: http://localhost:8080/metrics"
echo "    Raw engine metrics:          http://localhost:9100/metrics"
echo ""
echo "    Run this script again any time to send another burst of traffic."
echo ""
echo "==> To stop everything:"
echo "    docker compose -f $COMPOSE_FILE down"
