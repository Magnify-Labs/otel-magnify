import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const clientSource = readFileSync(new URL('../../src/api/client.ts', import.meta.url), 'utf8')
const websocketSource = readFileSync(new URL('../../src/api/websocket.ts', import.meta.url), 'utf8')
const appSource = readFileSync(new URL('../../src/App.tsx', import.meta.url), 'utf8')
const protectedRouteSource = readFileSync(
  new URL('../../src/components/ProtectedRoute.tsx', import.meta.url),
  'utf8',
)
const sessionSource = readFileSync(new URL('../../src/api/session.ts', import.meta.url), 'utf8')

test('API client relies on httpOnly cookie sessions instead of localStorage bearer headers', () => {
  assert.match(clientSource, /withCredentials:\s*true/)
  assert.doesNotMatch(clientSource, /localStorage\.getItem\(['"]token['"]\)/)
  assert.doesNotMatch(clientSource, /Authorization\s*=\s*`Bearer/)
})

test('WebSocket connects to same-origin /ws without putting a token in the URL', () => {
  assert.doesNotMatch(websocketSource, /localStorage\.getItem\(['"]token['"]\)/)
  assert.doesNotMatch(websocketSource, /\?token=/)
  assert.match(websocketSource, /new WebSocket\(`\$\{protocol\}\/\/\$\{window\.location\.host\}\/ws`\)/)
})

test('SPA session gates never persist or read bearer tokens from localStorage', () => {
  for (const [label, source] of [
    ['App.tsx', appSource],
    ['ProtectedRoute.tsx', protectedRouteSource],
    ['session.ts', sessionSource],
  ] as const) {
    assert.doesNotMatch(source, /localStorage\.(getItem|setItem)\(['"]token['"]/, label)
  }
})
