# Report export API

Status: draft contract for P2.16 implementation.

The repository does not currently ship an OpenAPI/Swagger document or generated API client. The report export contract is represented by:

- Go API structs in `pkg/models/report_export.go`.
- The community signing extension point in `pkg/ext/report_signer.go`.
- Handwritten frontend TypeScript types in `frontend/src/types.ts`.
- The handwritten frontend API client in `frontend/src/api/client.ts`.

## Evidence pack preview

`POST /api/reports/evidence-pack`

Request headers:

- `Content-Type: application/json`

Request body: `ReportExportRequest`.

Response:

- `200 OK`
- `Content-Type: application/json`
- Body: `EvidencePack`

## Evidence pack export

`POST /api/reports/evidence-pack/export?format=markdown|csv|pdf`

Request headers:

- `Content-Type: application/json`

Request body: `ReportExportRequest`.

The `format` query parameter selects the exported content type. Implementations may default to `markdown` when it is omitted, but clients should send it explicitly.

Successful responses:

| Format | Status | Content-Type | Body |
|---|---:|---|---|
| `markdown` | `200 OK` | `text/markdown; charset=utf-8` | Deterministic Markdown evidence pack |
| `csv` | `200 OK` | `text/csv; charset=utf-8` | Deterministic flat evidence table |
| `pdf` | `200 OK` | `application/pdf` | Deterministic minimal PDF evidence pack |

Export responses should include `Content-Disposition: attachment; filename="evidence-pack-<inputs_hash_12>.<ext>"`.

If PDF export is unavailable, the endpoint returns `501 Not Implemented` with JSON body:

```json
{
  "error": "pdf export unavailable",
  "code": "pdf_unavailable",
  "fallback_format": "markdown"
}
```

## Request body

`ReportExportRequest` fields:

- `schema_version`: currently `report_export_request.v1`.
- `report_type`: currently `evidence_pack`.
- `scope`: exactly one of `workload_ids`, `group_id`, or `selector`, with optional `since` and `until` bounds.
- `include`: booleans for `workload_summary`, `config_history`, `current_config`, `config_plan`, `drift_findings`, `version_intelligence`, `alerts`, `workload_events`, `rollback_readiness`, and `audit_verification`.
- `redaction`: currently `strict`; `none` is reserved and unsupported in community v1.

## EvidencePack response body

`EvidencePack` fields:

- `schema_version`: currently `evidence_pack.v1`.
- `generated_at`: UTC RFC3339/RFC3339Nano timestamp chosen by the service.
- `inputs_hash`: lowercase SHA-256 over canonical request and resolved inputs.
- `report_hash`: lowercase SHA-256 over the final redacted canonical payload excluding signatures.
- `scope`: resolved scope metadata.
- `sections`: ordered `EvidenceSection[]` with stable section IDs and `EvidenceItem[]`.
- `signatures`: optional report payload signatures; community uses `scheme=none` and `verifier=community-none`.
- `signed_audit`: optional signed audit-chain verification metadata when an enterprise verifier is wired.
- `warnings`: optional deterministic warning codes such as `pdf_minimal_renderer` or truncation notices.

CSV exports use these fixed v1 columns:

```text
section_id,item_id,resource,resource_id,observed_at,severity,summary,key,value,content_hash,redacted
```

## Error responses

Common JSON error bodies use `APIErrorResponse` shape plus report-export-specific fields where needed:

- `400 invalid JSON body`
- `400 unsupported report_type`
- `400 unsupported export format`
- `400 invalid scope`
- `400 invalid time range`
- `400 unsupported redaction mode`
- `401 unauthorized`
- `403 forbidden`
- `404 workload not found`
- `409 report scope empty`
- `413 request body too large`
- `500 failed to build evidence pack`
- `501 pdf export unavailable` with `fallback_format: "markdown"`
- `503 audit unavailable`
- `503 report signing unavailable`
