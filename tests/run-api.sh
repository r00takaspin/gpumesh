#!/bin/bash
# Run BDD API tests against a local coordinator.
# Usage: ./tests/run-api.sh
# The coordinator must be running with TEST_MODE=true on :8080.
set -euo pipefail

COORDINATOR_URL="${COORDINATOR_URL:-http://localhost:8080}"

# Verify coordinator is reachable.
if ! curl -sf "$COORDINATOR_URL/health" > /dev/null; then
  echo "ERROR: Coordinator not reachable at $COORDINATOR_URL"
  echo "Start it with: TEST_MODE=true MESH_DB=/tmp/gpumesh-test.db go run ./cmd/coordinator"
  exit 1
fi

cd "$(dirname "$0")"

if [ ! -d "node_modules" ]; then
  echo "Installing dependencies..."
  npm ci
fi

echo "Running BDD API tests..."
npx cucumber-js -p api --format progress
