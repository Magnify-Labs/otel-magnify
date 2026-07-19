\set ON_ERROR_STOP on

DO $assert$
DECLARE
    goose_version BIGINT;
BEGIN
    WITH ranked_version_states AS (
        SELECT
            id,
            version_id,
            is_applied,
            ROW_NUMBER() OVER (PARTITION BY version_id ORDER BY id DESC) AS state_rank
        FROM goose_db_version
        WHERE version_id > 0
    )
    SELECT COALESCE((
        SELECT version_id
        FROM ranked_version_states
        WHERE state_rank = 1
          AND is_applied
        ORDER BY id DESC
        LIMIT 1
    ), 0)
    INTO goose_version;

    IF goose_version <> 26 THEN
        RAISE EXCEPTION 'Goose version mismatch: got %, expected 26', goose_version;
    END IF;

    IF (SELECT COUNT(*) FROM configs
        WHERE id = 'upgrade-marker-config'
          AND name = 'upgrade-marker-config'
          AND content = $collector$receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch: {}
exporters:
  debug: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
$collector$
          AND created_at = TIMESTAMP '2025-01-02 03:04:05'
          AND created_by = 'postgres-lifecycle-fixture'
          AND kind = 'saved'
          AND status = 'ready'
          AND category = 'lifecycle'
          AND stack = 'otel-collector'
          AND description = 'PostgreSQL lifecycle fixture'
          AND variables = '[]'
          AND tags = '["upgrade-fixture"]'
          AND source_type = 'manual'
          AND git_url IS NULL
          AND git_provider IS NULL
          AND git_ref IS NULL
          AND git_path IS NULL
          AND commit_sha IS NULL
          AND imported_at IS NULL) <> 1 THEN
        RAISE EXCEPTION 'config fixture data or metadata mismatch';
    END IF;

    IF (SELECT COUNT(*) FROM workloads
        WHERE id = 'upgrade-marker-workload'
          AND fingerprint_source = 'k8s'
          AND fingerprint_keys = '{"k8s.namespace.name":"upgrade-fixture","service.name":"otel-collector"}'
          AND display_name = 'upgrade-marker-workload'
          AND type = 'collector'
          AND version = '0.98.0'
          AND status = 'disconnected'
          AND last_seen_at = TIMESTAMP '2025-01-02 03:04:06'
          AND labels = '{"environment":"upgrade-test","team":"observability"}'
          AND active_config_id = 'upgrade-marker-config'
          AND active_config_hash = 'upgrade-fixture-hash'
          AND remote_config_status::jsonb = '{"status":"failed","config_hash":"upgrade-fixture-hash","error_message":"Remote config error details redacted","updated_at":"2025-01-02T03:05:06Z"}'::jsonb
          AND POSITION('SECRET_TOKEN=fixture-only' IN remote_config_status) = 0
          AND available_components = '{"receivers":["otlp"],"processors":["batch"],"exporters":["debug"]}'
          AND accepts_remote_config
          AND retention_until = TIMESTAMP '2099-01-01 00:00:00'
          AND archived_at IS NULL) <> 1 THEN
        RAISE EXCEPTION 'workload fixture data, metadata, or migrated status mismatch';
    END IF;

    IF (SELECT COUNT(*) FROM workload_configs
        WHERE workload_id = 'upgrade-marker-workload') <> 1 THEN
        RAISE EXCEPTION 'expected exactly one workload config history row';
    END IF;

    IF (SELECT COUNT(*) FROM workload_configs
        WHERE workload_id = 'upgrade-marker-workload'
          AND config_id = 'upgrade-marker-config'
          AND applied_at = TIMESTAMP '2025-01-02 03:04:07'
          AND status = 'applied'
          AND error_message IS NULL
          AND pushed_by = 'postgres-lifecycle-fixture'
          AND label = 'upgrade-marker'
          AND push_id = 'upgrade-fixture-push'
          AND submitted_at = TIMESTAMP '2025-01-02 03:04:07'
          AND sent_at = TIMESTAMP '2025-01-02 03:04:08'
          AND opamp_status_timeout_at IS NULL
          AND rollback_of_push_id = ''
          AND instance_statuses = '[]') <> 1 THEN
        RAISE EXCEPTION 'workload config history data or metadata mismatch';
    END IF;

    IF (SELECT COUNT(*) FROM workload_attributes
        WHERE workload_id = 'upgrade-marker-workload') <> 4
       OR (SELECT COUNT(*) FROM workload_attributes
           WHERE workload_id = 'upgrade-marker-workload'
             AND (source, key, value) IN (
                 ('fingerprint', 'k8s.namespace.name', 'upgrade-fixture'),
                 ('fingerprint', 'service.name', 'otel-collector'),
                 ('label', 'environment', 'upgrade-test'),
                 ('label', 'team', 'observability')
             )) <> 4 THEN
        RAISE EXCEPTION 'workload attribute projection mismatch';
    END IF;
END
$assert$;
