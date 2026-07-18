import test from 'node:test'
import assert from 'node:assert/strict'
import { parseCapabilitiesDocument } from '../../src/api/capabilitiesContract.ts'

test('parses v1 capabilities and keeps unknown ids', () => {
  const parsed = parseCapabilitiesDocument({
    api_version: 'v1',
    capabilities: [
      { id: 'future.capability', state: 'enabled' },
      { id: 'known.disabled', state: 'disabled', reason_code: 'future_reason' },
      { id: 'known.read_only', state: 'read_only', reason_code: 'read_only_mode' },
    ],
  })
  assert.equal(parsed.get('future.capability')?.state, 'enabled')
  assert.equal(parsed.get('known.disabled')?.reason_code, 'future_reason')
  assert.equal(parsed.get('known.read_only')?.state, 'read_only')
})

for (const [name, document] of [
  ['wrong version', { api_version: 'v2', capabilities: [] }],
  ['missing array', { api_version: 'v1' }],
  ['unknown state', { api_version: 'v1', capabilities: [{ id: 'a', state: 'future' }] }],
  ['enabled reason', { api_version: 'v1', capabilities: [{ id: 'a', state: 'enabled', reason_code: 'not_enabled' }] }],
  ['missing disabled reason', { api_version: 'v1', capabilities: [{ id: 'a', state: 'disabled' }] }],
  ['duplicate id', { api_version: 'v1', capabilities: [{ id: 'a', state: 'enabled' }, { id: 'a', state: 'enabled' }] }],
] as const) {
  test(`rejects ${name}`, () => {
    assert.throws(() => parseCapabilitiesDocument(document), /capabilit/i)
  })
}
