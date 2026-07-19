\set ON_ERROR_STOP on

DO $fixture$
DECLARE
    updated_rows INTEGER;
BEGIN
    UPDATE workloads
    SET remote_config_status = '{"status":"failed","config_hash":"upgrade-fixture-hash","error_message":"collector failed: SECRET_TOKEN=fixture-only","updated_at":"2025-01-02T03:05:06Z"}'
    WHERE id = 'upgrade-marker-workload';

    GET DIAGNOSTICS updated_rows = ROW_COUNT;
    IF updated_rows <> 1 THEN
        RAISE EXCEPTION 'legacy status fixture expected one workload, got %', updated_rows;
    END IF;
END
$fixture$;
