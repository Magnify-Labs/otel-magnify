\set ON_ERROR_STOP on

BEGIN;

INSERT INTO configs (
    id,
    name,
    content,
    created_at,
    created_by,
    kind,
    status,
    category,
    stack,
    description,
    variables,
    tags,
    source_type,
    git_url,
    git_provider,
    git_ref,
    git_path,
    commit_sha,
    imported_at
) VALUES (
    'upgrade-marker-config',
    'upgrade-marker-config',
    $collector$receivers:
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
$collector$,
    TIMESTAMP '2025-01-02 03:04:05',
    'postgres-lifecycle-fixture',
    'saved',
    'ready',
    'lifecycle',
    'otel-collector',
    'PostgreSQL lifecycle fixture',
    '[]',
    '["upgrade-fixture"]',
    'manual',
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    content = EXCLUDED.content,
    created_at = EXCLUDED.created_at,
    created_by = EXCLUDED.created_by,
    kind = EXCLUDED.kind,
    status = EXCLUDED.status,
    category = EXCLUDED.category,
    stack = EXCLUDED.stack,
    description = EXCLUDED.description,
    variables = EXCLUDED.variables,
    tags = EXCLUDED.tags,
    source_type = EXCLUDED.source_type,
    git_url = EXCLUDED.git_url,
    git_provider = EXCLUDED.git_provider,
    git_ref = EXCLUDED.git_ref,
    git_path = EXCLUDED.git_path,
    commit_sha = EXCLUDED.commit_sha,
    imported_at = EXCLUDED.imported_at;

INSERT INTO workloads (
    id,
    fingerprint_source,
    fingerprint_keys,
    display_name,
    type,
    version,
    status,
    last_seen_at,
    labels,
    active_config_id,
    active_config_hash,
    remote_config_status,
    available_components,
    accepts_remote_config,
    retention_until,
    archived_at
) VALUES (
    'upgrade-marker-workload',
    'k8s',
    '{"k8s.namespace.name":"upgrade-fixture","service.name":"otel-collector"}',
    'upgrade-marker-workload',
    'collector',
    '0.98.0',
    'disconnected',
    TIMESTAMP '2025-01-02 03:04:06',
    '{"environment":"upgrade-test","team":"observability"}',
    'upgrade-marker-config',
    'upgrade-fixture-hash',
    NULL,
    '{"receivers":["otlp"],"processors":["batch"],"exporters":["debug"]}',
    TRUE,
    TIMESTAMP '2099-01-01 00:00:00',
    NULL
)
ON CONFLICT (id) DO UPDATE SET
    fingerprint_source = EXCLUDED.fingerprint_source,
    fingerprint_keys = EXCLUDED.fingerprint_keys,
    display_name = EXCLUDED.display_name,
    type = EXCLUDED.type,
    version = EXCLUDED.version,
    status = EXCLUDED.status,
    last_seen_at = EXCLUDED.last_seen_at,
    labels = EXCLUDED.labels,
    active_config_id = EXCLUDED.active_config_id,
    active_config_hash = EXCLUDED.active_config_hash,
    remote_config_status = EXCLUDED.remote_config_status,
    available_components = EXCLUDED.available_components,
    accepts_remote_config = EXCLUDED.accepts_remote_config,
    retention_until = EXCLUDED.retention_until,
    archived_at = EXCLUDED.archived_at;

DELETE FROM workload_configs
WHERE workload_id = 'upgrade-marker-workload';

INSERT INTO workload_configs (
    workload_id,
    config_id,
    applied_at,
    status,
    error_message,
    pushed_by,
    label,
    push_id,
    submitted_at,
    sent_at,
    opamp_status_timeout_at,
    rollback_of_push_id,
    instance_statuses
) VALUES (
    'upgrade-marker-workload',
    'upgrade-marker-config',
    TIMESTAMP '2025-01-02 03:04:07',
    'applied',
    NULL,
    'postgres-lifecycle-fixture',
    'upgrade-marker',
    'upgrade-fixture-push',
    TIMESTAMP '2025-01-02 03:04:07',
    TIMESTAMP '2025-01-02 03:04:08',
    NULL,
    '',
    '[]'
);

DELETE FROM workload_attributes
WHERE workload_id = 'upgrade-marker-workload';

INSERT INTO workload_attributes (workload_id, source, key, value) VALUES
    ('upgrade-marker-workload', 'fingerprint', 'k8s.namespace.name', 'upgrade-fixture'),
    ('upgrade-marker-workload', 'fingerprint', 'service.name', 'otel-collector'),
    ('upgrade-marker-workload', 'label', 'environment', 'upgrade-test'),
    ('upgrade-marker-workload', 'label', 'team', 'observability');

COMMIT;
