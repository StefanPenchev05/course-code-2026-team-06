#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

pg_container=
redis_container=
cleanup() {
    local result=$?
    if (( result != 0 )); then
        if [[ -n "$pg_container" ]]; then docker logs --tail 15 "$pg_container" >&2 || true; fi
        if [[ -n "$redis_container" ]]; then docker logs --tail 15 "$redis_container" >&2 || true; fi
    fi
    if [[ -n "$pg_container" ]]; then docker rm -fv "$pg_container" >/dev/null || true; fi
    if [[ -n "$redis_container" ]]; then docker rm -fv "$redis_container" >/dev/null || true; fi
}
trap cleanup EXIT

pg_network=(-p 127.0.0.1::5432)
redis_network=(-p 127.0.0.1::6379)
if [[ -n "${TUTTI_TEST_DOCKER_NETWORK:-}" ]]; then
    pg_network=(--network "$TUTTI_TEST_DOCKER_NETWORK" --network-alias integration-postgres)
    redis_network=(--network "$TUTTI_TEST_DOCKER_NETWORK" --network-alias integration-redis)
fi
pg_container=$(docker run -d --tmpfs /var/lib/postgresql/data:rw,size=256m \
    "${pg_network[@]}" -e POSTGRES_PASSWORD=tutti_test -e POSTGRES_DB=tutti_test postgres:17)
redis_container=$(docker run -d --tmpfs /data:rw,size=32m \
    "${redis_network[@]}" redis:8.2-alpine redis-server --save "" --appendonly no)

wait_ready() {
    local attempt
    for ((attempt=0; attempt<60; attempt++)); do
        if "$@" >/dev/null 2>&1; then return 0; fi
        sleep 0.25
    done
    echo "Test service failed to start" >&2
    return 1
}

wait_ready docker exec "$pg_container" pg_isready -h 127.0.0.1 -U postgres -d tutti_test
wait_ready docker exec "$redis_container" redis-cli ping
if [[ -n "${TUTTI_TEST_DOCKER_NETWORK:-}" ]]; then
    pg_port=integration-postgres:5432
    redis_port=integration-redis:6379
else
    pg_port=$(docker port "$pg_container" 5432/tcp)
    redis_port=$(docker port "$redis_container" 6379/tcp)
fi
export TUTTI_TEST_DATABASE_URL="postgres://postgres:tutti_test@${pg_port}/tutti_test?sslmode=disable"
export TUTTI_TEST_REDIS_URL="redis://${redis_port}/0"
migrate -path migrations -database "$TUTTI_TEST_DATABASE_URL" up
go test "$@" -count=1 -v ./tests/integration
psql -X -v ON_ERROR_STOP=1 "$TUTTI_TEST_DATABASE_URL" -f ops/notifications.sql
