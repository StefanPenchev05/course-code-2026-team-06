#!/usr/bin/env bash
set -euo pipefail
cd /src
unformatted=$(gofmt -l cmd internal tests)
if [[ -n "$unformatted" ]]; then
    printf 'Run gofmt on:\n%s\n' "$unformatted"
    exit 1
fi
go vet ./...
go test -race ./...
go build ./...
bash tests/integration/test_identity.sh -race
python3 tests/integration/test_schema.py
