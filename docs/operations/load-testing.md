# Load testing

`scripts/load-test-5000.sh` runs a reproducible, local-only OpAMP scenario
with 5,000 simulated collectors. It starts an isolated Docker Compose project,
waits for the API health check, then runs `cmd/opamp-load` from a Go container
on that project's Docker network. It publishes no host ports, so it can run
alongside a local Docker Compose or production-like stack.

This is a capacity test, not a production diagnostic. Run it only on a host
with enough CPU, memory, file descriptors, and Docker resources for 5,000
concurrent WebSocket connections.

## Safety boundary

The script requires the explicit `LOAD_TEST_CONFIRM=5000` acknowledgement and
test-only values for `DB_DSN`, `JWT_SECRET`, and `OPAMP_SHARED_SECRET`. It
never prints those values. Each run uses a unique Compose project name and its
cleanup deliberately omits `docker compose down -v`, so it does not remove
volumes.

Do not point this scenario at a shared or production database, listener, or
secret. The supplied `DB_DSN` must resolve `postgres` on the isolated Compose
network.

## Run the scenario

From the repository root, use test-only values such as:

```bash
LOAD_TEST_CONFIRM=5000 \
DB_DSN='postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable' \
JWT_SECRET='load-test-jwt-secret-at-least-32-bytes' \
OPAMP_SHARED_SECRET='load-test-opamp-token' \
./scripts/load-test-5000.sh
```

The default ramp takes five minutes and the hold period takes ten minutes. Set
`LOAD_TEST_RAMP` and `LOAD_TEST_HOLD` to Go duration values when a shorter
local smoke run is needed; keep the collector count fixed at 5,000 for the
acceptance scenario.

```bash
LOAD_TEST_RAMP=1m LOAD_TEST_HOLD=5m \
  LOAD_TEST_CONFIRM=5000 \
  DB_DSN='postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable' \
  JWT_SECRET='load-test-jwt-secret-at-least-32-bytes' \
  OPAMP_SHARED_SECRET='load-test-opamp-token' \
  ./scripts/load-test-5000.sh
```

Set `LOAD_TEST_OUTPUT_DIR` to retain artifacts at a specific location. Without
it, the script creates a directory under the system temporary directory.

## Result and evidence

`cmd/opamp-load` writes one JSON object to standard output:

```json
{"attempted":5000,"connected":5000,"failed":0,"disconnected":5000}
```

The script fails unless all 5,000 connections succeed and cleanly disconnect.
Its artifact directory contains:

- `summary.json`: client connection counters.
- `docker-stats.txt`: one resource snapshot for the application and PostgreSQL containers.
- `pg-stat-activity.txt`: PostgreSQL connection-state counts, excluding the diagnostic query itself, used to confirm the SQL pool stays at or below the configured maximum of 40.
- `opamp-errors.txt`: application log lines matching error, failed, or panic.

Review `opamp-errors.txt` and `pg-stat-activity.txt` with the JSON summary
before treating a run as accepted. A non-empty error file, more than 40 active
database connections, or any failed client requires investigation.
