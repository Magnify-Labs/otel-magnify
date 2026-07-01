import test from 'node:test'
import assert from 'node:assert/strict'

import { safeRollbackReasonText } from '../../src/lib/safeRemoteErrorText.ts'

test('redacts unsafe rollback reason details while keeping operator labels', () => {
  const rendered = safeRollbackReasonText(
    `rollback failed: SECRET_TOKEN=abc123 tenant-a.internal authorization=Bearer super-secret
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: https://collector-prod-eu.example.com:4317`,
  )

  assert.equal(rendered.includes('SECRET_TOKEN'), false)
  assert.equal(rendered.includes('tenant-a.internal'), false)
  assert.equal(rendered.toLowerCase().includes('authorization=bearer'), false)
  assert.equal(rendered.includes('super-secret'), false)
  assert.match(rendered, /redacted credential/)
  assert.match(rendered, /redacted endpoint/)
  assert.match(rendered, /configuration error/)
})

test('redacts tenant-like identifiers and bare host endpoints in rollback reasons', () => {
  const rendered = safeRollbackReasonText(
    'rollback blocked for tenant_prod_eu because collector-prod-eu:4317 rejected the bundle',
  )

  assert.equal(rendered.includes('tenant_prod_eu'), false)
  assert.equal(rendered.includes('collector-prod-eu:4317'), false)
  assert.match(rendered, /redacted tenant/)
  assert.match(rendered, /redacted endpoint/)
})
