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
test-only values for `DB_DSN`, `JWT_SECRET`, and `OPAMP_SHARED_SECRET`.
`DB_DSN` is an intent guard: its supplied value is deliberately ignored and
replaced with `postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable`
inside the unique isolated Compose network. Inherited `POSTGRES_PASSWORD` and
database pool settings are also ignored. Each run uses a unique Compose project
name and its cleanup deliberately omits `docker compose down -v`, so it does
not remove volumes.

Do not point this scenario at a shared or production database, listener, or
secret. Never provide a production `DB_DSN`; the required value is not used.

## Run the scenario

From the repository root, use test-only values such as:

```bash
LOAD_TEST_CONFIRM=5000 \
DB_DSN='required-but-ignored' \
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
  DB_DSN='required-but-ignored' \
  JWT_SECRET='load-test-jwt-secret-at-least-32-bytes' \
  OPAMP_SHARED_SECRET='load-test-opamp-token' \
  ./scripts/load-test-5000.sh
```

Set `LOAD_TEST_OUTPUT_DIR` to retain artifacts at a specific location. Without
it, the script creates a directory under the system temporary directory.

## Result and evidence

`cmd/opamp-load` writes a final JSON lifecycle summary to standard output:

```json
{"attempted":5000,"connected":5000,"failed":0,"cancelled":0,"disconnected":5000,"stop_failed":0,"interrupted":false}
```

`connected` is the cumulative count of collectors that completed their initial
connection; `disconnected` is the cumulative count of those that stopped
cleanly. `failed` counts failures before an initial connection, while
`stop_failed` records failures after one. `cancelled` counts clients cancelled
before connecting and `interrupted` marks a SIGINT or SIGTERM run. The client
prints this final summary even when interrupted, then exits non-zero.

Before the hold period ends, the script waits for a `ready.json` summary that
proves all 5,000 clients are connected, captures the following evidence, and
enforces the acceptance criteria. It fails unless the final lifecycle counters
are exact, PostgreSQL has no more than 40 application connections, and no
application log lines match `error`, `failed`, or `panic`.

Its artifact directory contains:

- `ready.json`: client counters at the start of the hold period.
- `summary.json`: final client lifecycle counters after graceful stopping.
- `opamp-load.stderr`: client diagnostics.
- `compose.log`: raw application Compose logs, including any log-collection failure.
- `docker-stats.txt`: one resource snapshot for the application and PostgreSQL containers.
- `pg-stat-activity.txt`: PostgreSQL connection count and the enforced limit, excluding the diagnostic query itself.
- `opamp-errors.txt`: application log lines matching error, failed, or panic while clients are held.

The script checks these artifacts before the clients stop. Preserve them with
`LOAD_TEST_OUTPUT_DIR` when a failed acceptance run needs investigation. Before
each run, it removes prior artifacts with these names while preserving the
selected output directory.
