# PostgreSQL 18 lifecycle

This runbook defines the Community edition database contract and the manual
upgrade and rollback boundaries. Test every procedure against a non-production
copy before scheduling a production change.

## 1. Supported version and TLS

Community supports PostgreSQL 18.x only. Point `DB_DSN` at a PostgreSQL 18
server and use certificate and hostname verification in production, for
example `sslmode=verify-full` with the appropriate root certificate. The
Compose `sslmode=disable` setting is only for its private local Docker network.

## 2. Database role and migration ownership

The database role needs `CONNECT` on the application database. The role that
runs migrations must also own every application schema and object it may
change, including tables, indexes, sequences, and `goose_db_version`. Goose
migrations need to `CREATE`, `ALTER`, `DROP`, and `CREATE INDEX`. Granting only
`USAGE, CREATE` on a schema is insufficient because those privileges do not
allow a role to alter or drop an existing object owned by another role.

The current deployment uses one application role for startup migrations and
normal queries. That role owns the application schemas and objects, so it
retains the DDL capability Goose needs for the entire process lifetime. This
larger blast radius is explicitly accepted for this P0. Do not simulate role
separation with incomplete grants: startup would fail on a later migration.

A future hardening can use an ephemeral owner/migrator role and a separate
runtime role. That runtime role would receive only:

- `CONNECT` on the database;
- `USAGE` on the application schemas;
- `SELECT, INSERT, UPDATE, DELETE` on application tables; and
- `USAGE, SELECT, UPDATE` on application sequences.

The migrator would still own the schemas, application objects, and Goose
metadata, and would need to grant the same table and sequence privileges for
new objects through default privileges. This split is outside the P0 scope.

## 3. Connection-pool budget and statistics

Set `DB_MAX_OPEN_CONNS` from a database-wide budget, not from the application
default alone. Leave capacity for provider administration, migrations, and
other database clients, then divide the remaining connections across every
application process. `DB_MAX_IDLE_CONNS` must not exceed
`DB_MAX_OPEN_CONNS`; use `DB_CONN_MAX_IDLE_TIME_SECONDS` and
`DB_CONN_MAX_LIFETIME_SECONDS` to retire idle and long-lived connections.

An authenticated caller with `settings:manage` can inspect numeric pool
counters at `GET /api/system/database`. Monitor `open_connections`, `in_use`,
`idle`, `wait_count`, and `wait_duration_ms`. The endpoint never returns the
DSN, host, user, or SQL errors.

## 4. Pre-upgrade backup

Create a PostgreSQL custom-format backup before every application upgrade and
make sure `pg_restore` can read its table of contents:

```bash
(
  set -euo pipefail
  umask 077

  backup_file="magnify-pre-upgrade-$(date -u +%Y%m%dT%H%M%SZ).dump"
  contents_file="${backup_file}.list"
  PGDATABASE="${DB_DSN:?set DB_DSN through your secret workflow}" \
    pg_dump --format=custom --file="${backup_file}"
  pg_restore --list "${backup_file}" >"${contents_file}"
  test -s "${backup_file}" && test -s "${contents_file}"
)
```

Store the dump, its list, the application image digest, and the PostgreSQL
server version together according to your own retention policy. A successful
dump and list are necessary checks, not proof that recovery works; rehearse a
restore into a separate database.

## 5. Move a v0.7.1 database from PostgreSQL 16 to 18

For a v0.7.1 deployment still using PostgreSQL 16, separate the database-major
move from the application upgrade:

1. Pin the exact v0.7.1 application artifact and record its digest.
2. Back up the PostgreSQL 16 database with a custom-format dump and inspect it
   with `pg_restore --list`, or use a provider migration mechanism with the
   same source and destination guarantees.
3. Provision a distinct, empty PostgreSQL 18 instance. Restore or migrate the
   PostgreSQL 16 data into that instance, remapping ownership when necessary
   so the current application role owns every application schema and object;
   do not reuse the source physical data directory.
4. Run the exact same v0.7.1 artifact against the restored PostgreSQL 18
   instance. Validate login, inventory, configuration history, and a governed
   config push before accepting the database-major move.
5. Stop v0.7.1, create and inspect a new custom-format backup from PostgreSQL
   18, and retain it as the pre-application-upgrade recovery point.
6. Only then start the new application artifact and let its Goose migrations
   run against PostgreSQL 18.

Keep the PostgreSQL 16 source unchanged until the PostgreSQL 18 validation and
backup have both succeeded.

## 6. Stop the single application pod

Keep Helm `replicaCount: 1` while the OpAMP registry and live connections are
process-local. Before the final backup and application migration, stop the
unique application pod and confirm it is no longer writing. This creates an
application-level downtime window. After the replacement pod becomes ready,
collectors reconnect and rebuild their process-local OpAMP connections.

The chart uses `Recreate`, so an ordinary Helm upgrade also terminates the old
pod before starting the replacement. Do not introduce a second application
replica as an upgrade shortcut.

## 7. Roll back with a separate restored database

Rollback means both an old application artifact and the pre-upgrade database
state:

1. Stop the failed application pod and preserve the failed database for
   investigation.
2. Create a separate, empty PostgreSQL 18 database or instance.
3. Restore the pre-upgrade custom-format backup into that separate target and
   validate the restore and application-object ownership.
4. Point the previous pinned application artifact at the restored target, then
   start the single pod and validate readiness and the critical user flow.

Do not point the old artifact at a database already changed by newer
migrations, and do not restore over the failed database in place.

## 8. Automation does not restore data

`helm upgrade --atomic` can roll Kubernetes release objects back, but it does
not restore PostgreSQL schemas or rows. A failed atomic upgrade can therefore
leave a newer database behind an older application artifact. Goose `Down`
migrations also do not constitute a data restore: a down migration may be
lossy and cannot recreate operational writes made during the upgrade window.
Use the separate-database restore procedure above.

## 9. Recovery objectives

Community does not promise point-in-time recovery, an RPO, or an RTO. Those
capabilities and objectives depend on the selected PostgreSQL provider or
operator, backup frequency, retention, restore testing, and the operator's
documented incident procedure.

## 10. Rotate the PostgreSQL password

Coordinate password rotation in this order:

1. Change the actual database role password through the provider control plane
   or a protected `ALTER ROLE` operation. Do not expose the password in shell
   history, logs, or Helm values.
2. Update the Kubernetes Secret referenced by `database.existingSecret` with
   the matching DSN.
3. Change `deployment.secretRevision` to a new opaque value and run the Helm
   upgrade so the single pod restarts with the new Secret.
4. Verify `/readyz`, login, and `GET /api/system/database` after restart.

The chart cannot detect external Secret content changes during rendering. Plan
for the short credential transition and use the provider's supported
zero-downtime mechanism if it offers one.

For the official PostgreSQL container, `POSTGRES_PASSWORD` initializes the
role only when the data directory is empty. Changing only that environment
variable on an already initialized volume does **not** change the existing
database role password.

## Never do this

Never change only the container tag from PostgreSQL 16 to PostgreSQL 18 while
mounting the same physical data directory. Never mount a PostgreSQL 16 volume
at the PostgreSQL 18 data path. Provision a distinct PostgreSQL 18 instance and
use dump/restore or a supported provider migration mechanism. Keep the old
volume until validation and the new PostgreSQL 18 backup are complete; do not
automate `docker compose down --volumes` against the old project.
