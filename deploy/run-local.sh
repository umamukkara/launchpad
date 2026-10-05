#!/usr/bin/env bash
# run-local.sh
#
# Runs the full LaunchPad + Prometheus + Grafana stack WITHOUT Docker and
# WITHOUT sudo. Use this when Docker Desktop can't be installed (e.g. a
# managed/no-admin-rights machine).
#
# It downloads plain Prometheus and Grafana binaries (official tarballs,
# no installer, no admin rights needed) into deploy/.local-stack/, builds
# the two Go services, starts all four as background processes, sends a
# burst of test traffic, and prints the URLs to open.
#
# Safe to re-run: if the stack is already up, it skips straight to sending
# another burst of traffic instead of starting a second copy of everything.
#
# Run from the repo root: ./deploy/run-local.sh
# Stop everything with:   ./deploy/stop-local.sh

set -euo pipefail

cd "$(dirname "$0")/.."   # repo root
STACK_DIR="deploy/.local-stack"
LOG_DIR="$STACK_DIR/logs"
PID_FILE="$STACK_DIR/pids.txt"

PROM_VERSION="3.5.5"
GRAFANA_VERSION="13.2.3"

wait_for() {
  local name="$1" url="$2" tries=30
  echo -n "==> Waiting for $name ($url) "
  until curl -s -o /dev/null "$url"; do
    tries=$((tries - 1))
    if [ "$tries" -le 0 ]; then
      echo "FAILED"
      echo "    $name never responded at $url."
      echo "    Check its log under: $LOG_DIR/"
      exit 1
    fi
    echo -n "."
    sleep 2
  done
  echo " up"
}

send_test_traffic() {
  echo "==> Sending test traffic..."
  echo "    - 50 REST calls to /api/engine/status"
  for _ in $(seq 1 50); do
    curl -s -o /dev/null "http://localhost:8080/api/engine/status"
  done

  echo "    - scheduling and igniting a launch (exercises REST + gRPC + WebSocket)"
  local launch_json launch_id
  launch_json=$(curl -s -X POST "http://localhost:8080/api/launches" \
    -d '{"vehicle":"vega-c","target_thrust_kn":2150,"duration_seconds":10}')
  launch_id=$(echo "$launch_json" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
  echo "      launch id: $launch_id"
  curl -s -X POST "http://localhost:8080/api/launches/$launch_id/ignite" > /dev/null
  echo "      ignited — burn runs for ~10s in the background"
}

print_urls() {
  echo ""
  echo "==> Open these in your browser:"
  echo ""
  echo "    Grafana dashboard:   http://localhost:3000"
  echo "                         (look for 'LaunchPad — live traffic')"
  echo "    Prometheus targets:  http://localhost:9090/targets"
  echo "    Raw mission-control metrics: http://localhost:8080/metrics"
  echo "    Raw engine metrics:          http://localhost:9100/metrics"
  echo ""
  echo "==> To stop everything:"
  echo "    ./deploy/stop-local.sh"
}

# --- if the stack is already running, just send more traffic and exit ---

if curl -s -o /dev/null "http://localhost:8080/healthz" 2>/dev/null; then
  echo "==> LaunchPad is already running. Sending another burst of traffic..."
  send_test_traffic
  print_urls
  exit 0
fi

# --- figure out which binaries to download ---

OS="$(uname -s)"
ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  arm64|aarch64) ARCH="arm64" ;;
  x86_64|amd64)  ARCH="amd64" ;;
  *) echo "Unrecognized CPU architecture: $ARCH_RAW"; exit 1 ;;
esac

if [ "$OS" != "Darwin" ]; then
  echo "This script is written for macOS. Detected: $OS."
  echo "The same approach works on Linux too, but the download URLs below"
  echo "are macOS-specific (darwin-*). Ask me to adjust them for Linux."
  exit 1
fi

mkdir -p "$STACK_DIR/bin" "$STACK_DIR/tools" "$STACK_DIR/prom-data" "$LOG_DIR"
: > "$PID_FILE"   # truncate; we're about to record fresh PIDs

PROM_DIR="$STACK_DIR/tools/prometheus-${PROM_VERSION}.darwin-${ARCH}"
GRAFANA_DIR="$STACK_DIR/tools/grafana-${GRAFANA_VERSION}"

# --- download Prometheus if we don't already have it ---

if [ ! -x "$PROM_DIR/prometheus" ]; then
  echo "==> Downloading Prometheus ${PROM_VERSION} (darwin-${ARCH})..."
  curl -sL --fail \
    "https://github.com/prometheus/prometheus/releases/download/v${PROM_VERSION}/prometheus-${PROM_VERSION}.darwin-${ARCH}.tar.gz" \
    -o "$STACK_DIR/tools/prometheus.tar.gz"
  tar -xzf "$STACK_DIR/tools/prometheus.tar.gz" -C "$STACK_DIR/tools"
  rm "$STACK_DIR/tools/prometheus.tar.gz"
else
  echo "==> Prometheus already downloaded, skipping."
fi

# --- download Grafana if we don't already have it ---

if [ ! -x "$GRAFANA_DIR/bin/grafana" ]; then
  echo "==> Downloading Grafana ${GRAFANA_VERSION} (darwin-${ARCH})..."
  curl -sL --fail \
    "https://dl.grafana.com/oss/release/grafana-${GRAFANA_VERSION}.darwin-${ARCH}.tar.gz" \
    -o "$STACK_DIR/tools/grafana.tar.gz"
  tar -xzf "$STACK_DIR/tools/grafana.tar.gz" -C "$STACK_DIR/tools"
  rm "$STACK_DIR/tools/grafana.tar.gz"
else
  echo "==> Grafana already downloaded, skipping."
fi

# --- build the Go services ---

echo "==> Building engine and missioncontrol..."
go build -o "$STACK_DIR/bin/engine" ./cmd/engine
go build -o "$STACK_DIR/bin/missioncontrol" ./cmd/missioncontrol

# --- prepare a Grafana provisioning folder with the right absolute paths ---
# (the checked-in deploy/grafana/provisioning assumes Docker's container
# paths; here we point it at this real machine's actual folder instead)

GRAFANA_PROVISIONING="$STACK_DIR/grafana-provisioning"
rm -rf "$GRAFANA_PROVISIONING"
mkdir -p "$GRAFANA_PROVISIONING/datasources" "$GRAFANA_PROVISIONING/dashboards"

# Note: we do NOT copy deploy/grafana/provisioning/datasources/datasource.yml
# here — that one points at "http://prometheus:9090", a hostname that only
# resolves inside Docker's network. Running locally, Prometheus is just on
# localhost, so we write our own copy of the datasource file with that URL.
cat > "$GRAFANA_PROVISIONING/datasources/datasource.yml" <<EOF
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://localhost:9090
    isDefault: true
    editable: false
EOF

DASHBOARDS_ABS_PATH="$(cd deploy/grafana/dashboards && pwd)"
cat > "$GRAFANA_PROVISIONING/dashboards/dashboards.yml" <<EOF
apiVersion: 1
providers:
  - name: launchpad
    orgId: 1
    folder: ""
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    options:
      path: ${DASHBOARDS_ABS_PATH}
EOF

# --- start everything in the background ---

echo "==> Starting engine..."
"$STACK_DIR/bin/engine" > "$LOG_DIR/engine.log" 2>&1 &
echo $! >> "$PID_FILE"

sleep 1

echo "==> Starting missioncontrol..."
"$STACK_DIR/bin/missioncontrol" > "$LOG_DIR/missioncontrol.log" 2>&1 &
echo $! >> "$PID_FILE"

echo "==> Starting Prometheus..."
"$PROM_DIR/prometheus" \
  --config.file=deploy/prometheus.local.yml \
  --storage.tsdb.path="$STACK_DIR/prom-data" \
  --web.listen-address=:9090 \
  > "$LOG_DIR/prometheus.log" 2>&1 &
echo $! >> "$PID_FILE"

echo "==> Starting Grafana..."
GF_PATHS_PROVISIONING="$(cd "$GRAFANA_PROVISIONING" && pwd)" \
GF_AUTH_ANONYMOUS_ENABLED=true \
GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer \
GF_SERVER_HTTP_PORT=3000 \
"$GRAFANA_DIR/bin/grafana" server --homepath="$(cd "$GRAFANA_DIR" && pwd)" \
  > "$LOG_DIR/grafana.log" 2>&1 &
echo $! >> "$PID_FILE"

echo "    PIDs saved to $PID_FILE (used by deploy/stop-local.sh)"

# --- wait for everything to come up ---

wait_for "mission-control" "http://localhost:8080/healthz"
wait_for "engine metrics"  "http://localhost:9100/metrics"
wait_for "prometheus"      "http://localhost:9090/-/ready"
wait_for "grafana"         "http://localhost:3000/api/health"

echo "==> Checking Prometheus scrape targets..."
TARGETS_JSON=$(curl -s "http://localhost:9090/api/v1/targets")
UP_COUNT=$(echo "$TARGETS_JSON" | grep -o '"health":"up"' | wc -l | tr -d ' ')
echo "    $UP_COUNT target(s) reporting healthy (expect 2: missioncontrol, engine)"

send_test_traffic

echo ""
echo "==> Done. Everything is running (no Docker, no sudo)."
print_urls
echo ""
echo "    Run this script again any time to send another burst of traffic —"
echo "    it will notice the stack is already up and skip straight to that."
