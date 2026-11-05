#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
: "${TEST_DATABASE_URL:?Set an isolated kaloq_v1_*_test PostgreSQL URL; tests reset only that database}"
(cd "$ROOT/apps/api" && python3 "$ROOT/scripts/with-toolchain.py" go test -race -count=1 ./... && python3 "$ROOT/scripts/with-toolchain.py" go vet ./...)
(cd "$ROOT/apps/web" && python3 "$ROOT/scripts/with-toolchain.py" npm run build)
